import { commands, window, workspace, type ExtensionContext } from "vscode";
import { Session } from "./session.ts";
import { pathSetting } from "./settings.ts";

let session: Session | undefined;

export async function activate(context: ExtensionContext): Promise<void> {
  const output = window.createOutputChannel("Shell LSP", { log: true });
  const current = new Session(context, output);
  session = current;
  context.subscriptions.push(
    output,
    commands.registerCommand("shellLsp.restart", () => current.restart()),
    commands.registerCommand("shellLsp.showOutput", () => {
      output.show();
    }),
    workspace.onDidChangeConfiguration((change) => {
      if (change.affectsConfiguration(pathSetting)) {
        void current.restart();
      }
    }),
  );
  await current.start();
}

export function deactivate(): Promise<void> | undefined {
  return session?.stop();
}
