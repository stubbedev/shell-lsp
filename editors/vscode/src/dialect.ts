import type { WorkspaceEdit } from "vscode-languageclient";
import { isFields } from "./fields.ts";

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

export function documentDialect(result: unknown): DocumentDialect {
  if (
    !isFields(result) ||
    typeof result["dialect"] !== "string" ||
    typeof result["source"] !== "string" ||
    typeof result["guessed"] !== "boolean" ||
    typeof result["description"] !== "string" ||
    typeof result["directive"] !== "boolean" ||
    !Array.isArray(result["dialects"]) ||
    !result["dialects"].every((name) => typeof name === "string")
  ) {
    throw new Error(`${documentDialectMethod} answered ${JSON.stringify(result)}, not a document dialect`);
  }
  return {
    dialect: result["dialect"],
    source: result["source"],
    guessed: result["guessed"],
    description: result["description"],
    directive: result["directive"],
    dialects: result["dialects"],
  };
}

function isWorkspaceEdit(value: unknown): value is WorkspaceEdit {
  return isFields(value) && (isFields(value["changes"]) || Array.isArray(value["documentChanges"]));
}

export function directiveEdit(result: unknown): WorkspaceEdit | null {
  if (result === null) {
    return null;
  }
  if (!isWorkspaceEdit(result)) {
    throw new Error(`${setDialectMethod} answered ${JSON.stringify(result)}, not a workspace edit`);
  }
  return result;
}

export function shellArgument(command: string, args: readonly unknown[]): string | null | undefined {
  if (args.length === 0) {
    return undefined;
  }
  const [shell] = args;
  if (args.length !== 1 || (shell !== null && typeof shell !== "string")) {
    throw new Error(`${command} takes no argument, a shell name, or null to remove the directive; got ${JSON.stringify(args)}`);
  }
  return shell;
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
