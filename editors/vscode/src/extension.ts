import { commands, window, workspace, type ExtensionContext } from "vscode";
import { DialectStatus } from "./dialectStatus.ts";
import { clientCommands } from "./ids.ts";
import { Session } from "./session.ts";
import { pathSetting } from "./settings.ts";

let session: Session | undefined;

export async function activate(context: ExtensionContext): Promise<void> {
  const output = window.createOutputChannel("Shell LSP", { log: true });
  const status = new DialectStatus(() => current.runningClient(), output);
  const current = new Session(context, output, () => {
    status.schedule();
  });
  session = current;
  context.subscriptions.push(
    output,
    status,
    commands.registerCommand(clientCommands.restart, () => current.restart()),
    commands.registerCommand(clientCommands.showOutput, () => {
      output.show();
    }),
    commands.registerCommand(clientCommands.chooseShell, (...args: unknown[]) => status.choose(args)),
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
