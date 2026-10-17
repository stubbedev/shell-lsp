import { commands, window, workspace } from "vscode";
import type { ExecuteCommandSignature, LanguageClient, Middleware } from "vscode-languageclient/node";
import { referenceLocations, referenceTarget } from "./commandArgs.ts";
import { serverCommands } from "./ids.ts";
import { argumentsFrom, needsPrompt, promptFor, recipeRun } from "./recipes.ts";
import { requireActiveShellDocument } from "./shellDocument.ts";

export function executeCommandMiddleware(client: () => LanguageClient): NonNullable<Middleware["executeCommand"]> {
  return async (command: string, args: unknown[], next: ExecuteCommandSignature): Promise<unknown> => {
    switch (command) {
      case serverCommands.showReferences:
        return showReferences(client(), args, next);
      case serverCommands.fixAll:
        return next(command, args.length > 0 ? args : [requireActiveShellDocument(client(), command).textDocument.uri]);
      case serverCommands.runBats:
        if (!workspace.isTrusted) {
          throw new Error("Running Bats tests is disabled in Restricted Mode; trust the workspace to run them.");
        }
        return next(command, args.length > 0 ? args : [requireActiveShellDocument(client(), command).textDocument]);
      case serverCommands.runRecipe:
        if (!workspace.isTrusted) {
          throw new Error("Running recipes is disabled in Restricted Mode; trust the workspace to run them.");
        }
        return runRecipe(args, next);
      default:
        return next(command, args);
    }
  };
}

async function runRecipe(args: unknown[], next: ExecuteCommandSignature): Promise<unknown> {
  const command = serverCommands.runRecipe;
  const run = recipeRun(command, args);
  if (!needsPrompt(run)) {
    return next(command, args);
  }
  const parameters = run.parameters ?? [];
  const answers: string[] = [];
  for (const parameter of parameters) {
    const answer = await window.showInputBox({ title: `Run ${run.name}`, prompt: promptFor(parameter), ignoreFocusOut: true });
    if (answer === undefined) {
      return undefined;
    }
    answers.push(answer);
  }
  return next(command, [{ uri: run.uri, name: run.name, arguments: argumentsFrom(parameters, answers) }]);
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
