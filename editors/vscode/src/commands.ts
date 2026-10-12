import { commands, languages, window, workspace } from "vscode";
import type { ExecuteCommandSignature, LanguageClient, Middleware } from "vscode-languageclient/node";
import { referenceLocations, referenceTarget } from "./commandArgs.ts";
import { documentSelector } from "./documents.ts";
import { serverCommands, type ServerCommand } from "./ids.ts";

export function executeCommandMiddleware(client: () => LanguageClient): NonNullable<Middleware["executeCommand"]> {
  return async (command: string, args: unknown[], next: ExecuteCommandSignature): Promise<unknown> => {
    switch (command) {
      case serverCommands.showReferences:
        return showReferences(client(), args, next);
      case serverCommands.fixAll:
        return next(command, args.length > 0 ? args : [activeDocumentUri(client(), command)]);
      case serverCommands.runBats:
        if (!workspace.isTrusted) {
          throw new Error("Running Bats tests is disabled in Restricted Mode; trust the workspace to run them.");
        }
        return next(command, args.length > 0 ? args : [{ uri: activeDocumentUri(client(), command) }]);
      default:
        return next(command, args);
    }
  };
}

async function showReferences(client: LanguageClient, args: unknown[], next: ExecuteCommandSignature): Promise<unknown> {
  const command = serverCommands.showReferences;
  const target = referenceTarget(command, args);
  const locations = referenceLocations(command, await next(command, args)).map((location) =>
    client.protocol2CodeConverter.asLocation(location),
  );
  await commands.executeCommand(
    "editor.action.showReferences",
    client.protocol2CodeConverter.asUri(target.uri),
    client.protocol2CodeConverter.asPosition(target.position),
    locations,
  );
  return locations;
}

function activeDocumentUri(client: LanguageClient, command: ServerCommand): string {
  const document = window.activeTextEditor?.document;
  if (document === undefined || languages.match(documentSelector, document) === 0) {
    throw new Error(`${command} needs an active shell script editor`);
  }
  return client.code2ProtocolConverter.asUri(document.uri);
}
