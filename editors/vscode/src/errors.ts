export function describeError(error: unknown): string {
  if (!(error instanceof Error)) {
    return String(error);
  }
  return error.cause === undefined ? error.message : `${error.message}: ${describeError(error.cause)}`;
}
