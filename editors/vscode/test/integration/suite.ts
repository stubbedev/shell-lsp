import assert from "node:assert/strict";
import {
  commands,
  ConfigurationTarget,
  languages,
  Location,
  MarkdownString,
  Position,
  ProcessExecution,
  tasks,
  window,
  workspace,
  WorkspaceEdit,
  type CodeLens,
  type Diagnostic,
  type DocumentSymbol,
  type Hover,
  type TextEdit,
  type Uri,
  type WorkspaceFolder,
} from "vscode";

const timeoutMs = 60_000;

async function waitFor<T>(what: string, probe: () => T | PromiseLike<T>, done: (value: T) => boolean): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await probe();
    if (done(value)) {
      return value;
    }
    if (Date.now() > deadline) {
      throw new Error(`timed out after ${String(timeoutMs)} ms waiting for ${what}; last saw ${JSON.stringify(value)}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}

function codeOf(diagnostic: Diagnostic): string {
  const code = typeof diagnostic.code === "object" ? diagnostic.code.value : diagnostic.code;
  return String(code);
}

function hasCode(uri: Uri, code: string): boolean {
  return languages.getDiagnostics(uri).some((diagnostic) => codeOf(diagnostic) === code);
}

function hoverText(hovers: readonly Hover[]): string {
  return hovers
    .flatMap((hover) => hover.contents)
    .filter((content) => content instanceof MarkdownString)
    .map((content) => content.value)
    .join("\n");
}

async function showsDiagnostics(uri: Uri): Promise<void> {
  await waitFor("SC2086 on the unquoted $1", () => hasCode(uri, "SC2086"), Boolean);
}

async function showsHover(uri: Uri): Promise<void> {
  const hovers = await waitFor(
    "a hover on echo",
    () => commands.executeCommand<Hover[]>("vscode.executeHoverProvider", uri, new Position(2, 3)),
    (found) => hoverText(found).includes("echo"),
  );
  assert.ok(hoverText(hovers).length > 0);
}

async function peeksReferences(uri: Uri): Promise<void> {
  const lenses = await commands.executeCommand<CodeLens[]>("vscode.executeCodeLensProvider", uri, 100);
  const lens = lenses.find((candidate) => candidate.range.start.line === 1 && candidate.command !== undefined);
  assert.ok(lens?.command, `no resolved code lens on greet among ${JSON.stringify(lenses)}`);
  assert.equal(lens.command.command, "shellLsp.showReferences");
  assert.equal(lens.command.title, "2 references");
  const args: unknown[] = lens.command.arguments ?? [];
  const locations = await commands.executeCommand(lens.command.command, ...args);
  assert.ok(Array.isArray(locations));
  assert.equal(locations.length, 2);
  for (const location of locations) {
    assert.ok(location instanceof Location);
    assert.equal(location.uri.toString(), uri.toString());
  }
  assert.deepEqual(
    locations.map((location: Location) => location.range.start.line),
    [4, 5],
  );
}

async function appliesSettings(uri: Uri): Promise<void> {
  const settings = workspace.getConfiguration("shellLsp");
  await settings.update("lint.exclude", ["SC2086"], ConfigurationTarget.Global);
  await waitFor("SC2086 to disappear once excluded", () => hasCode(uri, "SC2086"), (present) => !present);
  await settings.update("lint.exclude", undefined, ConfigurationTarget.Global);
  await waitFor("SC2086 to return once no longer excluded", () => hasCode(uri, "SC2086"), Boolean);
}

async function formatsWithEditorconfig(folder: WorkspaceFolder): Promise<void> {
  const uri = fileIn(folder, "cases.sh");
  const document = await workspace.openTextDocument(uri);
  const edits = await waitFor(
    "formatting edits for cases.sh",
    () => commands.executeCommand<TextEdit[] | undefined>("vscode.executeFormatDocumentProvider", uri, { tabSize: 2, insertSpaces: true }),
    (found) => (found ?? []).length > 0,
  );
  const edit = new WorkspaceEdit();
  edit.set(uri, edits ?? []);
  assert.ok(await workspace.applyEdit(edit));
  assert.equal(document.getText(), '#!/bin/sh\ncase "$1" in\n  a) echo a ;;\nesac\n');
}

async function fixesAll(uri: Uri): Promise<void> {
  const document = await workspace.openTextDocument(uri);
  await commands.executeCommand("shellLsp.fixAll");
  const fixed = await waitFor("fix all to rewrite the backticks", () => document.getText(), (text) => text.includes("now=$(date)"));
  assert.ok(fixed.includes("greet $1\n"), "fix all applied a fix that changes behaviour");
}

async function outlinesBatsTests(folder: WorkspaceFolder, name: string, test: string): Promise<void> {
  const uri = fileIn(folder, name);
  const document = await workspace.openTextDocument(uri);
  assert.equal(document.languageId, "shellscript", `${name} is not a shell script`);
  await waitFor(
    `the outline of ${name} to list the test "${test}"`,
    () => commands.executeCommand<DocumentSymbol[] | undefined>("vscode.executeDocumentSymbolProvider", uri),
    (symbols) => (symbols ?? []).some((symbol) => symbol.name.includes(test)),
  );
}

async function setsTheShell(folder: WorkspaceFolder): Promise<void> {
  const document = await workspace.openTextDocument(fileIn(folder, "dialect.sh"));
  await window.showTextDocument(document);
  await waitFor(
    "the server to accept a shell choice for dialect.sh",
    async () => {
      try {
        await commands.executeCommand("shellLsp.chooseShell", "dash");
        return document.getText();
      } catch {
        return "";
      }
    },
    (text) => text === "#!/bin/sh\n# shellcheck shell=dash\necho hi\n",
  );
  await commands.executeCommand("shellLsp.chooseShell", null);
  assert.equal(document.getText(), "#!/bin/sh\necho hi\n");
}

async function leavesTheHostShellToTheHost(folder: WorkspaceFolder): Promise<void> {
  const document = await workspace.openTextDocument(fileIn(folder, "Makefile"));
  assert.equal(document.languageId, "makefile");
  await window.showTextDocument(document);
  await assert.rejects(Promise.resolve(commands.executeCommand("shellLsp.chooseShell", "dash")), /a directive cannot choose it/);
  assert.equal(document.getText(), "SHELL := /bin/bash\n\nall:\n\techo hi\n");
}

async function listsRecipesAsTasks(): Promise<void> {
  const found = await waitFor(
    "the Makefile's all target as a task",
    () => tasks.fetchTasks({ type: "shell-lsp" }),
    (list) => list.some((task) => task.name === "all" && task.source === "make"),
  );
  const all = found.find((task) => task.name === "all");
  assert.ok(all?.execution instanceof ProcessExecution);
  assert.deepEqual([all.execution.process, ...all.execution.args.slice(-1)], ["make", "all"]);
}

function fileIn(folder: WorkspaceFolder, name: string): Uri {
  return folder.uri.with({ path: `${folder.uri.path}/${name}` });
}

export async function run(): Promise<void> {
  const folder = workspace.workspaceFolders?.[0];
  assert.ok(folder, "the test workspace is not open");
  await outlinesBatsTests(folder, "arithmetic.bats", "adds numbers");
  await outlinesBatsTests(folder, "extensionless", "has no extension");

  const uri = fileIn(folder, "sample.sh");
  const document = await workspace.openTextDocument(uri);
  assert.equal(document.languageId, "shellscript");
  await window.showTextDocument(document);

  await showsDiagnostics(uri);
  await showsHover(uri);
  await peeksReferences(uri);
  await appliesSettings(uri);
  await fixesAll(uri);
  await formatsWithEditorconfig(folder);
  await setsTheShell(folder);
  await leavesTheHostShellToTheHost(folder);
  await listsRecipesAsTasks();
}
