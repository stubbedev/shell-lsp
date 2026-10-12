export type Parse<In, Out> = (value: In) => Out | undefined;

export function parseResult<T>(method: string, expected: string, parse: Parse<unknown, T>, result: unknown): T {
  const parsed = parse(result);
  if (parsed === undefined) {
    throw new Error(`${method} answered ${JSON.stringify(result)}, not ${expected}`);
  }
  return parsed;
}

export function parseArgs<T>(command: string, expected: string, parse: Parse<readonly unknown[], T>, args: readonly unknown[]): T {
  const parsed = parse(args);
  if (parsed === undefined) {
    throw new Error(`${command} takes ${expected}, got ${JSON.stringify(args)}`);
  }
  return parsed;
}
