import { window, workspace } from "vscode";
import type { TerminalRun } from "./recipes.ts";

export function openTerminal(run: TerminalRun): void {
  if (!workspace.isTrusted) {
    throw new Error("Running recipes is disabled in Restricted Mode; trust the workspace to run them.");
  }
  const [program, ...args] = run.argv;
  window.createTerminal({ name: run.name, shellPath: program, shellArgs: args, cwd: run.cwd }).show();
}
