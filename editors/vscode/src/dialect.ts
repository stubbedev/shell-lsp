import type { WorkspaceEdit } from "vscode-languageclient";
import { isFields, isList, isMember, isString } from "./fields.ts";
import { repository } from "./ids.ts";
import { parseArgs, parseResult } from "./parse.ts";
import { isTextWorkspaceEdit } from "./protocol.ts";
import { dialectSetting } from "./settings.ts";

export const documentDialectMethod = "shellLsp/documentDialect";
export const setDialectMethod = "shellLsp/setDialect";
export const dialectDocsUrl = `${repository}#dialects`;
export const hostShellDocsUrl = `${repository}#shell-in-other-files`;
export const learnLabel = "How is the shell detected?";

const scriptSources = [
  "default",
  "override",
  "editorconfig",
  "directive",
  "shebang",
  "completionTag",
  "fileName",
  "extension",
  "languageId",
  "modeline",
] as const;
const hostSources = ["hostSetting", "hostDefault"] as const;

export type ScriptSource = (typeof scriptSources)[number];
export type HostSource = (typeof hostSources)[number];

interface Detected<Source> {
  readonly dialect: string;
  readonly source: Source;
  readonly guessed: boolean;
  readonly description: string;
}

export interface HostDialect extends Detected<HostSource> {
  readonly selectable: false;
}

export interface ScriptDialect extends Detected<ScriptSource> {
  readonly selectable: true;
  readonly directive: boolean;
  readonly dialects: readonly string[];
}

export type DocumentDialect = HostDialect | ScriptDialect;

function parseDocumentDialect(value: unknown): DocumentDialect | undefined {
  if (!isFields(value)) {
    return undefined;
  }
  const { selectable, dialect, source, guessed, description, directive, dialects } = value;
  if (typeof dialect !== "string" || typeof guessed !== "boolean" || typeof description !== "string") {
    return undefined;
  }
  if (selectable === false) {
    return isMember(hostSources, source) ? { selectable, dialect, source, guessed, description } : undefined;
  }
  if (selectable !== true || !isMember(scriptSources, source) || typeof directive !== "boolean" || !isList(dialects, isString)) {
    return undefined;
  }
  return { selectable, dialect, source, guessed, description, directive, dialects };
}

export function documentDialect(result: unknown): DocumentDialect {
  return parseResult(documentDialectMethod, "a document dialect", parseDocumentDialect, result);
}

export function directiveEdit(result: unknown): WorkspaceEdit | null {
  return parseResult(
    setDialectMethod,
    "a workspace edit of text or null",
    (value) => (value === null || isTextWorkspaceEdit(value) ? value : undefined),
    result,
  );
}

export function shellArgument(command: string, args: readonly unknown[]): string | null | undefined {
  if (args.length === 0) {
    return undefined;
  }
  return parseArgs(
    command,
    "no argument, a shell name, or null to remove the directive",
    ([shell, ...rest]) => (rest.length === 0 && (shell === null || isString(shell)) ? shell : undefined),
    args,
  );
}

export interface StatusText {
  readonly text: string;
  readonly tooltip: string;
}

export function statusText(found: DocumentDialect): StatusText {
  const action = found.selectable ? "Click to choose the shell." : "Click to see where the shell comes from.";
  return {
    text: found.guessed ? `${found.dialect} (guessed)` : found.dialect,
    tooltip: `Linted as ${found.dialect}: ${found.description}. ${action}`,
  };
}

export function hostExplanation(found: HostDialect): string {
  return (
    `Linted as ${found.dialect}, ${found.description}. ` +
    "The file around the shell decides it: SHELL in a Makefile, the recipe's shebang or set shell in a justfile, " +
    "the writer or a /* bash */ comment in a Nix file. Change it there; a # shellcheck shell= directive cannot."
  );
}

export type ShellChoice =
  | { readonly kind: "set"; readonly dialect: string }
  | { readonly kind: "remove" }
  | { readonly kind: "learn" };

export interface ShellPick {
  readonly label: string;
  readonly description: string;
  readonly choice: ShellChoice;
}

export function shellPicks(found: ScriptDialect): readonly ShellPick[] {
  const picks: ShellPick[] = found.dialects.map((dialect) => ({
    label: dialect,
    description: dialect === found.dialect ? "current" : "",
    choice: { kind: "set", dialect },
  }));
  if (found.directive) {
    picks.push({ label: "Remove the shell directive", description: "", choice: { kind: "remove" } });
  }
  picks.push({ label: learnLabel, description: "", choice: { kind: "learn" } });
  return picks;
}

export function pickPlaceholder(found: ScriptDialect): string {
  switch (found.source) {
    case "override":
      return `The ${dialectSetting} setting makes this ${found.dialect}; a directive only takes effect once that setting is cleared`;
    case "editorconfig":
      return `shell_variant in .editorconfig makes this ${found.dialect}; a directive only takes effect once that is removed`;
    case "default":
    case "directive":
    case "shebang":
    case "completionTag":
    case "fileName":
    case "extension":
    case "languageId":
    case "modeline":
      return `Linted as ${found.dialect}, ${found.description}. Choose the shell to record in a # shellcheck shell= directive`;
  }
}
