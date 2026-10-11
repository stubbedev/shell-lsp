import { createHash, randomUUID } from "node:crypto";
import { mkdir, readdir, rename, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { expectedChecksum, isReleaseAssetName, type ReleaseAsset } from "./release.ts";

export type FetchBytes = (url: string) => Promise<Uint8Array>;

export async function installServer(asset: ReleaseAsset, dir: string, fetchBytes: FetchBytes): Promise<string> {
  const sums = new TextDecoder("utf-8", { fatal: true }).decode(await fetchBytes(asset.checksumsUrl));
  const expected = expectedChecksum(sums, asset.name);
  const binary = await fetchBytes(asset.url);
  const actual = createHash("sha256").update(binary).digest("hex");
  if (actual !== expected) {
    throw new Error(`SHA-256 of ${asset.url} is ${actual} but SHA256SUMS lists ${expected}; refusing to install it`);
  }
  await mkdir(dir, { recursive: true });
  const target = join(dir, asset.name);
  const temporary = join(dir, `.${asset.name}.${randomUUID()}.tmp`);
  try {
    await writeFile(temporary, binary, { mode: 0o755, flag: "wx" });
    await rename(temporary, target);
  } catch (error) {
    await rm(temporary, { force: true });
    throw new Error(`installing ${asset.name} into ${dir}`, { cause: error });
  }
  await removeOtherReleases(dir, asset.name);
  return target;
}

async function removeOtherReleases(dir: string, keep: string): Promise<void> {
  const stale = (await readdir(dir)).filter((name) => isReleaseAssetName(name) && name !== keep);
  await Promise.all(stale.map((name) => rm(join(dir, name), { force: true })));
}
