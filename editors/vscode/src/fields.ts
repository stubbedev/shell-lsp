export type Fields = Readonly<Record<string, unknown>>;

export function isFields(value: unknown): value is Fields {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isList<T>(value: unknown, isItem: (item: unknown) => item is T): value is readonly T[] {
  return Array.isArray(value) && value.every(isItem);
}

export function isString(value: unknown): value is string {
  return typeof value === "string";
}
