import { constants } from "node:fs";
import { access, stat } from "node:fs/promises";
import { posix, win32, type PlatformPath } from "node:path";
import { pathSetting } from "./settings.ts";

export const serverName = "shell-lsp";

export type ServerSource = "setting" | "PATH" | "download";

export interface ServerBinary {
  readonly path: string;
  readonly source: ServerSource;
}

export function serverExecutable(platform: NodeJS.Platform): string {
  return platform === "win32" ? `${serverName}.exe` : serverName;
}

export interface Lookup {
  readonly platform: NodeJS.Platform;
  readonly configuredPath: string;
  readonly pathEnv: string | undefined;
  readonly downloaded: string | undefined;
}

export type ExecutableProbe = (path: string) => Promise<boolean>;

export async function findServer(lookup: Lookup, isExecutable: ExecutableProbe): Promise<ServerBinary | undefined> {
  const paths = lookup.platform === "win32" ? win32 : posix;
  if (lookup.configuredPath !== "") {
    return { path: await configuredServer(paths, lookup.configuredPath, isExecutable), source: "setting" };
  }
  const executable = serverExecutable(lookup.platform);
  for (const dir of (lookup.pathEnv ?? "").split(paths.delimiter)) {
    if (!paths.isAbsolute(dir)) {
      continue;
    }
    const candidate = paths.join(dir, executable);
    if (await isExecutable(candidate)) {
      return { path: candidate, source: "PATH" };
    }
  }
  if (lookup.downloaded !== undefined && (await isExecutable(lookup.downloaded))) {
    return { path: lookup.downloaded, source: "download" };
  }
  return undefined;
}

async function configuredServer(paths: PlatformPath, path: string, isExecutable: ExecutableProbe): Promise<string> {
  if (!paths.isAbsolute(path)) {
    throw new Error(`${pathSetting} must be an absolute path, got ${JSON.stringify(path)}`);
  }
  if (!(await isExecutable(path))) {
    throw new Error(`${pathSetting} is ${path}, which is not an executable file`);
  }
  return path;
}

const absentCodes: ReadonlySet<string> = new Set(["ENOENT", "ENOTDIR", "EACCES"]);

export async function isExecutableFile(path: string): Promise<boolean> {
  try {
    if (!(await stat(path)).isFile()) {
      return false;
    }
    await access(path, constants.X_OK);
    return true;
  } catch (error) {
    if (error instanceof Error && "code" in error && typeof error.code === "string" && absentCodes.has(error.code)) {
      return false;
    }
    throw new Error(`checking whether ${path} is executable`, { cause: error });
  }
}
