import type { Location, Position, Range } from "vscode-languageclient";
import { isFields } from "./fields.ts";

export interface ReferenceTarget {
  readonly uri: string;
  readonly position: Position;
}

function isPosition(value: unknown): value is Position {
  return isFields(value) && Number.isInteger(value["line"]) && Number.isInteger(value["character"]);
}

function isRange(value: unknown): value is Range {
  return isFields(value) && isPosition(value["start"]) && isPosition(value["end"]);
}

function isLocation(value: unknown): value is Location {
  return isFields(value) && typeof value["uri"] === "string" && isRange(value["range"]);
}

export function referenceTarget(command: string, args: readonly unknown[]): ReferenceTarget {
  const [target] = args;
  if (args.length !== 1 || !isFields(target) || typeof target["uri"] !== "string" || !isPosition(target["position"])) {
    throw new Error(`${command} takes one {uri, position} argument, got ${JSON.stringify(args)}`);
  }
  return { uri: target["uri"], position: target["position"] };
}

export function referenceLocations(command: string, result: unknown): readonly Location[] {
  if (!Array.isArray(result) || !result.every(isLocation)) {
    throw new Error(`${command} answered ${JSON.stringify(result)}, not a list of locations`);
  }
  return result;
}
