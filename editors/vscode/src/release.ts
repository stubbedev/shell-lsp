import { serverName } from "./serverBinary.ts";

const repository = "https://github.com/stubbedev/shell-lsp";
const assetPrefix = `${serverName}-v`;

export type Os = "linux" | "darwin" | "windows";
export type Arch = "amd64" | "arm64";

export interface ReleaseTarget {
  readonly os: Os;
  readonly arch: Arch;
}

export interface ReleaseAsset {
  readonly name: string;
  readonly url: string;
  readonly checksumsUrl: string;
}

export type ServerDownload =
  | { readonly kind: "release"; readonly asset: ReleaseAsset }
  | { readonly kind: "unsupported"; readonly platform: string };

const oses: ReadonlyMap<string, Os> = new Map([
  ["linux", "linux"],
  ["darwin", "darwin"],
  ["win32", "windows"],
]);
const arches: ReadonlyMap<string, Arch> = new Map([
  ["x64", "amd64"],
  ["arm64", "arm64"],
]);

export const releaseTargets: readonly ReleaseTarget[] = [...oses.values()].flatMap((os) =>
  [...arches.values()].map((arch) => ({ os, arch })),
);

export function serverDownload(version: string, platform: string, arch: string): ServerDownload {
  const os = oses.get(platform);
  const goArch = arches.get(arch);
  if (os === undefined || goArch === undefined) {
    return { kind: "unsupported", platform: `${platform}-${arch}` };
  }
  return { kind: "release", asset: releaseAsset(version, { os, arch: goArch }) };
}

export function releaseAsset(version: string, target: ReleaseTarget): ReleaseAsset {
  const base = `${repository}/releases/download/v${version}`;
  const suffix = target.os === "windows" ? ".exe" : "";
  const name = `${assetPrefix}${version}-${target.os}-${target.arch}${suffix}`;
  return { name, url: `${base}/${name}`, checksumsUrl: `${base}/SHA256SUMS` };
}

export function isReleaseAssetName(name: string): boolean {
  return name.startsWith(assetPrefix);
}

const checksumLine = /^([0-9a-f]{64}) [ *](.+)$/;

export function parseChecksums(text: string): ReadonlyMap<string, string> {
  const sums = new Map<string, string>();
  for (const [index, line] of text.split("\n").entries()) {
    if (line.trim() === "") {
      continue;
    }
    const match = checksumLine.exec(line);
    const [, digest, name] = match ?? [];
    if (digest === undefined || name === undefined) {
      throw new Error(`SHA256SUMS line ${String(index + 1)} is not "<sha256>  <file>": ${JSON.stringify(line)}`);
    }
    if (sums.has(name)) {
      throw new Error(`SHA256SUMS lists ${name} more than once`);
    }
    sums.set(name, digest);
  }
  return sums;
}

export function releaseAssetProblems(version: string, files: readonly string[], sums: string): readonly string[] {
  const expected = releaseTargets.map((target) => releaseAsset(version, target).name);
  const built = files.filter(isReleaseAssetName).sort();
  const listed = [...parseChecksums(sums).keys()].sort();
  return [
    ...expected.filter((name) => !built.includes(name)).map((name) => `the extension downloads ${name}, which was not built`),
    ...expected.filter((name) => !listed.includes(name)).map((name) => `the extension downloads ${name}, which SHA256SUMS does not list`),
    ...built.filter((name) => !expected.includes(name)).map((name) => `${name} was built, but the extension never downloads it`),
    ...listed.filter((name) => !expected.includes(name)).map((name) => `SHA256SUMS lists ${name}, which the extension never downloads`),
  ];
}

export function expectedChecksum(text: string, name: string): string {
  const digest = parseChecksums(text).get(name);
  if (digest === undefined) {
    throw new Error(`SHA256SUMS has no entry for ${name}`);
  }
  return digest;
}
