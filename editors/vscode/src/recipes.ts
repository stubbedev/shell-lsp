import { isFields, isList, isString } from "./fields.ts";
import { parseArgs, parseResult } from "./parse.ts";

export interface RecipeParameter {
  readonly name: string;
  readonly default?: string;
  readonly optional: boolean;
  readonly variadic: boolean;
}

export interface RecipeRun {
  readonly uri: string;
  readonly name: string;
  readonly arguments?: readonly string[];
  readonly parameters?: readonly RecipeParameter[];
}

export interface RecipeListing {
  readonly uri: string;
  readonly path: string;
  readonly name: string;
  readonly tool: string;
  readonly argv: readonly string[];
  readonly cwd: string;
  readonly parameters: readonly RecipeParameter[];
}

function isParameter(value: unknown): value is RecipeParameter {
  if (!isFields(value)) {
    return false;
  }
  const { name, default: fallback, optional, variadic } = value;
  return isString(name) && (fallback === undefined || isString(fallback)) && typeof optional === "boolean" && typeof variadic === "boolean";
}

export function recipeRun(command: string, args: readonly unknown[]): RecipeRun {
  return parseArgs(
    command,
    "one {uri, name, arguments?, parameters?} argument",
    ([run, ...rest]) => {
      if (rest.length > 0 || !isFields(run)) {
        return undefined;
      }
      const { uri, name, arguments: given, parameters } = run;
      if (!isString(uri) || !isString(name)) {
        return undefined;
      }
      if (given !== undefined && !isList(given, isString)) {
        return undefined;
      }
      if (parameters !== undefined && !isList(parameters, isParameter)) {
        return undefined;
      }
      return { uri, name, ...(given === undefined ? {} : { arguments: given }), ...(parameters === undefined ? {} : { parameters }) };
    },
    args,
  );
}

export function recipeListings(command: string, result: unknown): readonly RecipeListing[] {
  const isListing = (value: unknown): value is RecipeListing => {
    if (!isFields(value)) {
      return false;
    }
    const { uri, path, name, tool, argv, cwd, parameters } = value;
    return [uri, path, name, tool, cwd].every(isString) && isList(argv, isString) && argv.length > 0 && isList(parameters, isParameter);
  };
  return parseResult(command, "a list of recipes", (value) => (isList(value, isListing) ? value : undefined), result);
}

export function needsPrompt(run: RecipeRun): boolean {
  return run.arguments === undefined && (run.parameters?.length ?? 0) > 0;
}

export function runsWithoutArguments(listing: RecipeListing): boolean {
  return listing.parameters.every((p) => p.optional);
}

export function argumentsFrom(parameters: readonly RecipeParameter[], answers: readonly string[]): readonly string[] {
  const out: string[] = [];
  parameters.forEach((parameter, i) => {
    const answer = answers[i] ?? "";
    if (parameter.variadic) {
      out.push(...answer.split(/\s+/).filter((word) => word !== ""));
      return;
    }
    if (answer === "" && parameter.optional) {
      return;
    }
    out.push(answer);
  });
  return out;
}

export function promptFor(parameter: RecipeParameter): string {
  const extras = [parameter.variadic ? "separate several with spaces" : "", parameter.default === undefined ? "" : `default ${parameter.default}`]
    .filter((part) => part !== "")
    .join(", ");
  return extras === "" ? parameter.name : `${parameter.name} (${extras})`;
}
