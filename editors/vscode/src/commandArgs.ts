import type { Location, Position } from "vscode-languageclient";
import { isFields, isList } from "./fields.ts";
import { parseArgs, parseResult } from "./parse.ts";
import { isLocation, isPosition } from "./protocol.ts";

export interface ReferenceTarget {
  readonly uri: string;
  readonly position: Position;
}

export function referenceTarget(command: string, args: readonly unknown[]): ReferenceTarget {
  return parseArgs(
    command,
    "one {uri, position} argument",
    ([target, ...rest]) => {
      if (rest.length > 0 || !isFields(target)) {
        return undefined;
      }
      const { uri, position } = target;
      return typeof uri === "string" && isPosition(position) ? { uri, position } : undefined;
    },
    args,
  );
}

export function referenceLocations(command: string, result: unknown): readonly Location[] {
  return parseResult(command, "a list of locations", (value) => (isList(value, isLocation) ? value : undefined), result);
}
