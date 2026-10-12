import { isFields } from "./fields.ts";

export const section = "shellLsp";
export const pathKey = "path";
export const pathSetting = `${section}.${pathKey}`;
export const dialectSetting = `${section}.dialect`;

const clientOnlyKeys: ReadonlySet<string> = new Set([pathKey, "trace.server"]);

export function serverKeys(manifest: unknown): readonly string[] {
  const contributes = isFields(manifest) ? manifest["contributes"] : undefined;
  const configuration = isFields(contributes) ? contributes["configuration"] : undefined;
  if (!Array.isArray(configuration)) {
    throw new Error("the extension manifest has no contributes.configuration list");
  }
  const prefix = `${section}.`;
  return configuration.flatMap((category: unknown) => {
    const properties = isFields(category) ? category["properties"] : undefined;
    if (!isFields(properties)) {
      throw new Error(`contributes.configuration entry ${JSON.stringify(category)} has no properties`);
    }
    return Object.keys(properties)
      .filter((key) => key.startsWith(prefix))
      .map((key) => key.slice(prefix.length))
      .filter((key) => !clientOnlyKeys.has(key));
  });
}

export interface ScopedValues {
  readonly globalValue?: unknown;
  readonly workspaceValue?: unknown;
  readonly workspaceFolderValue?: unknown;
}

export function explicitSettings(
  keys: readonly string[],
  inspect: (key: string) => ScopedValues | undefined,
): Record<string, unknown> {
  const settings: Record<string, unknown> = {};
  for (const key of keys) {
    const scoped = inspect(key);
    const value = [scoped?.workspaceFolderValue, scoped?.workspaceValue, scoped?.globalValue].find(
      (candidate) => candidate !== undefined,
    );
    if (value !== undefined) {
      assign(settings, key.split("."), value);
    }
  }
  return settings;
}

function assign(target: Record<string, unknown>, path: readonly string[], value: unknown): void {
  const [head, ...rest] = path;
  if (head === undefined) {
    throw new Error("a setting key cannot be empty");
  }
  if (rest.length === 0) {
    target[head] = value;
    return;
  }
  const existing = target[head];
  const child: Record<string, unknown> = isFields(existing) ? { ...existing } : {};
  assign(child, rest, value);
  target[head] = child;
}
