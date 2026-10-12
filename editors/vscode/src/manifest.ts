import { isFields } from "./fields.ts";
import { section } from "./settings.ts";

export interface Manifest {
  readonly version: string;
  readonly serverKeys: readonly string[];
}

const versionPattern = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;
const serverScope = "resource";

export function parseManifest(manifest: unknown): Manifest {
  if (!isFields(manifest)) {
    throw new Error(`the extension manifest is ${JSON.stringify(manifest)}, not an object`);
  }
  return { version: releaseVersion(manifest["version"]), serverKeys: serverKeys(manifest["contributes"]) };
}

function releaseVersion(version: unknown): string {
  if (typeof version !== "string" || !versionPattern.test(version)) {
    throw new Error(`extension version ${JSON.stringify(version)} is not a release version like 1.2.3`);
  }
  return version;
}

function serverKeys(contributes: unknown): readonly string[] {
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
    return Object.entries(properties)
      .filter(([key, schema]) => key.startsWith(prefix) && isFields(schema) && schema["scope"] === serverScope)
      .map(([key]) => key.slice(prefix.length));
  });
}
