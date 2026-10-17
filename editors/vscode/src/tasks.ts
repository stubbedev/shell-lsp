import { ProcessExecution, Task, TaskScope, Uri, workspace, type CancellationToken, type TaskDefinition, type TaskProvider } from "vscode";
import { ExecuteCommandRequest, type LanguageClient } from "vscode-languageclient/node";
import { serverCommands } from "./ids.ts";
import { recipeListings, runsWithoutArguments, type RecipeListing } from "./recipes.ts";

export const taskType = "shell-lsp";

interface RecipeTaskDefinition extends TaskDefinition {
  readonly file: string;
  readonly recipe: string;
  readonly arguments?: readonly string[];
}

function isRecipeDefinition(definition: TaskDefinition): definition is RecipeTaskDefinition {
  const { file, recipe, arguments: given } = definition;
  return typeof file === "string" && typeof recipe === "string" && (given === undefined || (Array.isArray(given) && given.every((a) => typeof a === "string")));
}

export class RecipeTasks implements TaskProvider {
  private readonly client: () => LanguageClient | undefined;

  constructor(client: () => LanguageClient | undefined) {
    this.client = client;
  }

  async provideTasks(token: CancellationToken): Promise<Task[]> {
    const listings = await this.listings(token);
    return listings.filter(runsWithoutArguments).map((listing) => task({ type: taskType, file: relative(listing), recipe: listing.name }, listing, []));
  }

  async resolveTask(found: Task, token: CancellationToken): Promise<Task | undefined> {
    const definition = found.definition;
    if (!isRecipeDefinition(definition)) {
      return undefined;
    }
    const listing = (await this.listings(token)).find((l) => (l.path === definition.file || relative(l) === definition.file) && l.name === definition.recipe);
    return listing === undefined ? undefined : task(definition, listing, definition.arguments ?? []);
  }

  private async listings(token: CancellationToken): Promise<readonly RecipeListing[]> {
    const client = this.client();
    if (client === undefined || !workspace.isTrusted) {
      return [];
    }
    const command = serverCommands.listRecipes;
    return recipeListings(command, await client.sendRequest(ExecuteCommandRequest.type, { command, arguments: [] }, token));
  }
}

function relative(listing: RecipeListing): string {
  return workspace.asRelativePath(listing.path, false);
}

function task(definition: RecipeTaskDefinition, listing: RecipeListing, args: readonly string[]): Task {
  const [program, ...argv] = listing.argv;
  const folder = workspace.getWorkspaceFolder(Uri.file(listing.path)) ?? TaskScope.Workspace;
  return new Task(definition, folder, listing.name, listing.tool, new ProcessExecution(program ?? listing.tool, [...argv, ...args], { cwd: listing.cwd }));
}
