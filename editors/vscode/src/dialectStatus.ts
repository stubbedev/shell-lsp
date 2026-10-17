import {
  env,
  StatusBarAlignment,
  Uri,
  window,
  workspace,
  type Disposable,
  type LogOutputChannel,
  type StatusBarItem,
} from "vscode";
import { LSPErrorCodes, ResponseError, type LanguageClient } from "vscode-languageclient/node";
import {
  dialectDocsUrl,
  directiveEdit,
  documentDialect,
  documentDialectMethod,
  hostExplanation,
  hostShellDocsUrl,
  learnLabel,
  pickPlaceholder,
  setDialectMethod,
  shellArgument,
  shellPicks,
  statusText,
  type DocumentDialect,
  type StatusText,
} from "./dialect.ts";
import { describeError } from "./errors.ts";
import { clientCommands, dialectStatusItem, displayName } from "./ids.ts";
import { dialectSetting } from "./settings.ts";
import { activeShellDocument, requireActiveShellDocument, type ShellDocument } from "./shellDocument.ts";

const chooseShellCommand = clientCommands.chooseShell;
const refreshDelayMs = 300;
const cancellationCodes: ReadonlySet<number> = new Set([
  LSPErrorCodes.RequestCancelled,
  LSPErrorCodes.ServerCancelled,
  LSPErrorCodes.ContentModified,
]);

function isCancellation(error: unknown): boolean {
  return error instanceof ResponseError && cancellationCodes.has(error.code);
}

export class DialectStatus implements Disposable {
  private readonly item: StatusBarItem;
  private readonly subscriptions: Disposable[];
  private readonly client: () => LanguageClient | undefined;
  private readonly output: LogOutputChannel;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private generation = 0;

  constructor(client: () => LanguageClient | undefined, output: LogOutputChannel) {
    this.client = client;
    this.output = output;
    this.item = window.createStatusBarItem(dialectStatusItem, StatusBarAlignment.Right, 100);
    this.item.name = `${displayName} Dialect`;
    this.item.command = chooseShellCommand;
    this.subscriptions = [
      this.item,
      window.onDidChangeActiveTextEditor(() => {
        this.schedule();
      }),
      window.onDidChangeTextEditorSelection((change) => {
        if (change.textEditor === window.activeTextEditor) {
          this.schedule();
        }
      }),
      workspace.onDidOpenTextDocument(() => {
        this.schedule();
      }),
      workspace.onDidChangeTextDocument((change) => {
        if (change.document === window.activeTextEditor?.document) {
          this.schedule();
        }
      }),
      workspace.onDidChangeConfiguration((change) => {
        if (change.affectsConfiguration(dialectSetting)) {
          this.schedule();
        }
      }),
    ];
  }

  schedule(): void {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      void this.refresh();
    }, refreshDelayMs);
  }

  async choose(args: readonly unknown[]): Promise<void> {
    const shell = shellArgument(chooseShellCommand, args);
    const { client, target } = this.target();
    if (shell !== undefined) {
      await this.apply(client, target, shell);
      return;
    }
    const found = await this.request(client, target);
    if (!found.selectable) {
      if ((await window.showInformationMessage(hostExplanation(found), learnLabel)) === learnLabel) {
        await env.openExternal(Uri.parse(hostShellDocsUrl));
      }
      return;
    }
    const picked = await window.showQuickPick(shellPicks(found), { placeHolder: pickPlaceholder(found) });
    switch (picked?.choice.kind) {
      case undefined:
        return;
      case "set":
        await this.apply(client, target, picked.choice.dialect);
        return;
      case "remove":
        await this.apply(client, target, null);
        return;
      case "learn":
        await env.openExternal(Uri.parse(dialectDocsUrl));
        return;
    }
  }

  dispose(): void {
    clearTimeout(this.timer);
    for (const subscription of this.subscriptions) {
      subscription.dispose();
    }
  }

  private async refresh(): Promise<void> {
    const generation = ++this.generation;
    const client = this.client();
    const target = client === undefined ? undefined : activeShellDocument(client);
    if (client === undefined || target === undefined) {
      this.item.hide();
      return;
    }
    try {
      const found = await this.request(client, target);
      if (generation === this.generation) {
        this.show(statusText(found));
      }
    } catch (error: unknown) {
      if (generation !== this.generation || this.client() !== client || isCancellation(error)) {
        return;
      }
      const message = describeError(error);
      this.output.error(`Asking for the shell of ${target.document.uri.toString()}: ${message}`);
      this.show({ text: "$(warning) shell", tooltip: `Could not tell the shell of this document: ${message}` });
    }
  }

  private show({ text, tooltip }: StatusText): void {
    this.item.text = text;
    this.item.tooltip = tooltip;
    this.item.show();
  }

  private target(): { client: LanguageClient; target: ShellDocument } {
    const client = this.client();
    if (client === undefined) {
      throw new Error(`${chooseShellCommand} needs the ${displayName} server to be running`);
    }
    return { client, target: requireActiveShellDocument(client, chooseShellCommand) };
  }

  private async request(client: LanguageClient, target: ShellDocument): Promise<DocumentDialect> {
    const cursor = window.activeTextEditor?.document === target.document ? window.activeTextEditor.selection.active : undefined;
    const result: unknown = await client.sendRequest(documentDialectMethod, {
      textDocument: target.textDocument,
      ...(cursor === undefined ? {} : { position: client.code2ProtocolConverter.asPosition(cursor) }),
    });
    return documentDialect(result);
  }

  private async apply(client: LanguageClient, target: ShellDocument, shell: string | null): Promise<void> {
    const result = directiveEdit(
      await client.sendRequest(setDialectMethod, { textDocument: target.textDocument, dialect: shell }),
    );
    if (result === null) {
      return;
    }
    const edit = await client.protocol2CodeConverter.asWorkspaceEdit(result);
    if (!(await workspace.applyEdit(edit))) {
      throw new Error(`${chooseShellCommand} could not apply the directive edit to ${target.document.uri.toString()}`);
    }
    this.schedule();
  }
}
