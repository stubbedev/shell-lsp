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

const oses: Readonly<Partial<Record<string, Os>>> = { linux: "linux", darwin: "darwin", win32: "windows" };
const arches: Readonly<Partial<Record<string, Arch>>> = { x64: "amd64", arm64: "arm64" };

export function serverDownload(version: string, platform: string, arch: string): ServerDownload {
  const os = oses[platform];
  const goArch = arches[arch];
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

export function expectedChecksum(text: string, name: string): string {
  const digest = parseChecksums(text).get(name);
  if (digest === undefined) {
    throw new Error(`SHA256SUMS has no entry for ${name}`);
  }
  return digest;
}
