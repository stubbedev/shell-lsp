import type { WorkspaceEdit } from "vscode-languageclient";
import { isFields, isList, isString } from "./fields.ts";
import { parseArgs, parseResult } from "./parse.ts";
import { isTextWorkspaceEdit } from "./protocol.ts";

export const documentDialectMethod = "shellLsp/documentDialect";
export const setDialectMethod = "shellLsp/setDialect";
export const dialectDocsUrl = "https://github.com/stubbedev/shell-lsp#dialects";

export interface DocumentDialect {
  readonly dialect: string;
  readonly source: string;
  readonly guessed: boolean;
  readonly description: string;
  readonly directive: boolean;
  readonly dialects: readonly string[];
}

function parseDocumentDialect(value: unknown): DocumentDialect | undefined {
  if (!isFields(value)) {
    return undefined;
  }
  const { dialect, source, guessed, description, directive, dialects } = value;
  if (
    typeof dialect !== "string" ||
    typeof source !== "string" ||
    typeof guessed !== "boolean" ||
    typeof description !== "string" ||
    typeof directive !== "boolean" ||
    !isList(dialects, isString)
  ) {
    return undefined;
  }
  return { dialect, source, guessed, description, directive, dialects };
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
  return {
    text: found.guessed ? `${found.dialect} (guessed)` : found.dialect,
    tooltip: `Linted as ${found.dialect}: ${found.description}. Click to choose the shell.`,
  };
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

export function shellPicks(found: DocumentDialect): readonly ShellPick[] {
  const picks: ShellPick[] = found.dialects.map((dialect) => ({
    label: dialect,
    description: dialect === found.dialect ? "current" : "",
    choice: { kind: "set", dialect },
  }));
  if (found.directive) {
    picks.push({ label: "Remove the shell directive", description: "", choice: { kind: "remove" } });
  }
  picks.push({ label: "How is the shell detected?", description: "", choice: { kind: "learn" } });
  return picks;
}

export function pickPlaceholder(found: DocumentDialect): string {
  switch (found.source) {
    case "override":
      return `The shellLsp.dialect setting makes this ${found.dialect}; a directive only takes effect once that setting is cleared`;
    case "editorconfig":
      return `shell_variant in .editorconfig makes this ${found.dialect}; a directive only takes effect once that is removed`;
    default:
      return `Linted as ${found.dialect}, ${found.description}. Choose the shell to record in a # shellcheck shell= directive`;
  }
}
