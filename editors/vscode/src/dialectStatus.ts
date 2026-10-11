import {
  env,
  languages,
  StatusBarAlignment,
  Uri,
  window,
  workspace,
  type Disposable,
  type LogOutputChannel,
  type StatusBarItem,
  type TextDocument,
} from "vscode";
import type { LanguageClient } from "vscode-languageclient/node";
import {
  dialectDocsUrl,
  directiveEdit,
  documentDialect,
  documentDialectMethod,
  pickPlaceholder,
  setDialectMethod,
  shellArgument,
  shellPicks,
  statusText,
  type DocumentDialect,
} from "./dialect.ts";
import { documentSelector } from "./documents.ts";
import { section } from "./settings.ts";

export const chooseShellCommand = "shellLsp.chooseShell";
const refreshDelayMs = 300;
const dialectSetting = `${section}.dialect`;

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
    this.item = window.createStatusBarItem("shellLsp.dialect", StatusBarAlignment.Right, 100);
    this.item.name = "Shell LSP Dialect";
    this.item.command = chooseShellCommand;
    this.subscriptions = [
      this.item,
      window.onDidChangeActiveTextEditor(() => {
        this.schedule();
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
    const { client, document } = this.target();
    if (shell !== undefined) {
      await this.apply(client, document, shell);
      return;
    }
    const found = await this.request(client, document);
    const picked = await window.showQuickPick(shellPicks(found), { placeHolder: pickPlaceholder(found) });
    switch (picked?.choice.kind) {
      case undefined:
        return;
      case "set":
        await this.apply(client, document, picked.choice.dialect);
        return;
      case "remove":
        await this.apply(client, document, null);
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
    const document = window.activeTextEditor?.document;
    if (client === undefined || document === undefined || !isShellDocument(document)) {
      this.item.hide();
      return;
    }
    try {
      const found = await this.request(client, document);
      if (generation !== this.generation) {
        return;
      }
      const { text, tooltip } = statusText(found);
      this.item.text = text;
      this.item.tooltip = tooltip;
      this.item.show();
    } catch (error: unknown) {
      if (generation === this.generation) {
        this.item.hide();
      }
      this.output.debug(`Not showing the dialect of ${document.uri.toString()}: ${String(error)}`);
    }
  }

  private target(): { client: LanguageClient; document: TextDocument } {
    const client = this.client();
    if (client === undefined) {
      throw new Error(`${chooseShellCommand} needs the Shell LSP server to be running`);
    }
    const document = window.activeTextEditor?.document;
    if (document === undefined || !isShellDocument(document)) {
      throw new Error(`${chooseShellCommand} needs an active shell script editor`);
    }
    return { client, document };
  }

  private async request(client: LanguageClient, document: TextDocument): Promise<DocumentDialect> {
    const result: unknown = await client.sendRequest(documentDialectMethod, {
      textDocument: { uri: client.code2ProtocolConverter.asUri(document.uri) },
    });
    return documentDialect(result);
  }

  private async apply(client: LanguageClient, document: TextDocument, shell: string | null): Promise<void> {
    const result = directiveEdit(
      await client.sendRequest(setDialectMethod, {
        textDocument: { uri: client.code2ProtocolConverter.asUri(document.uri) },
        dialect: shell,
      }),
    );
    if (result === null) {
      return;
    }
    const edit = await client.protocol2CodeConverter.asWorkspaceEdit(result);
    if (!(await workspace.applyEdit(edit))) {
      throw new Error(`${chooseShellCommand} could not apply the directive edit to ${document.uri.toString()}`);
    }
    this.schedule();
  }
}

function isShellDocument(document: TextDocument): boolean {
  return languages.match(documentSelector, document) > 0;
}
