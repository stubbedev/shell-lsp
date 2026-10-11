import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, test } from "node:test";
import { installServer, type FetchBytes } from "../../src/install.ts";
import { releaseAsset } from "../../src/release.ts";

const asset = releaseAsset("1.2.3", { os: "linux", arch: "amd64" });
const binary = new TextEncoder().encode("#!/bin/sh\necho shell-lsp\n");
const digest = createHash("sha256").update(binary).digest("hex");

function sums(lines: Readonly<Record<string, string>>): Uint8Array {
  return new TextEncoder().encode(
    Object.entries(lines)
      .map(([name, sum]) => `${sum}  ${name}\n`)
      .join(""),
  );
}

function fakeNetwork(responses: ReadonlyMap<string, Uint8Array>): { fetchBytes: FetchBytes; fetched: string[] } {
  const fetched: string[] = [];
  return {
    fetched,
    fetchBytes: (url) => {
      fetched.push(url);
      const body = responses.get(url);
      return body === undefined ? Promise.reject(new Error(`HTTP 404 for ${url}`)) : Promise.resolve(body);
    },
  };
}

describe("installServer", () => {
  let root = "";
  let dir = "";

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "shell-lsp-install-"));
    dir = join(root, "storage");
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
  });

  test("installs a verified binary as an executable file", async () => {
    const { fetchBytes } = fakeNetwork(
      new Map([
        [asset.checksumsUrl, sums({ "shell-lsp-v1.2.3-linux-arm64": "f".repeat(64), [asset.name]: digest })],
        [asset.url, binary],
      ]),
    );
    const installed = await installServer(asset, dir, fetchBytes);
    assert.equal(installed, join(dir, asset.name));
    assert.deepEqual(await readFile(installed), Buffer.from(binary));
    assert.equal((await stat(installed)).mode & 0o111, 0o111);
    assert.deepEqual(await readdir(dir), [asset.name]);
  });

  test("refuses a binary whose SHA-256 differs from SHA256SUMS and writes nothing", async () => {
    const { fetchBytes } = fakeNetwork(
      new Map([
        [asset.checksumsUrl, sums({ [asset.name]: "0".repeat(64) })],
        [asset.url, binary],
      ]),
    );
    await assert.rejects(installServer(asset, dir, fetchBytes), (error: unknown) => {
      assert.ok(error instanceof Error);
      assert.match(error.message, new RegExp(`SHA-256 of .*${asset.name} is ${digest} but SHA256SUMS lists 0{64}`));
      return true;
    });
    await assert.rejects(readdir(dir), { code: "ENOENT" });
  });

  test("does not download the binary when SHA256SUMS does not list it", async () => {
    const { fetchBytes, fetched } = fakeNetwork(
      new Map([
        [asset.checksumsUrl, sums({ "shell-lsp-v1.2.3-darwin-amd64": digest })],
        [asset.url, binary],
      ]),
    );
    await assert.rejects(installServer(asset, dir, fetchBytes), /SHA256SUMS has no entry for shell-lsp-v1\.2\.3-linux-amd64/);
    assert.deepEqual(fetched, [asset.checksumsUrl]);
    await assert.rejects(readdir(dir), { code: "ENOENT" });
  });

  test("fails when SHA256SUMS cannot be downloaded", async () => {
    const { fetchBytes, fetched } = fakeNetwork(new Map([[asset.url, binary]]));
    await assert.rejects(installServer(asset, dir, fetchBytes), /HTTP 404 for .*SHA256SUMS/);
    assert.deepEqual(fetched, [asset.checksumsUrl]);
  });

  test("fails when the binary cannot be downloaded and leaves no file behind", async () => {
    const { fetchBytes } = fakeNetwork(new Map([[asset.checksumsUrl, sums({ [asset.name]: digest })]]));
    await assert.rejects(installServer(asset, dir, fetchBytes), /HTTP 404 for .*linux-amd64/);
    await assert.rejects(readdir(dir), { code: "ENOENT" });
  });

  test("rejects a SHA256SUMS file that is not UTF-8", async () => {
    const { fetchBytes } = fakeNetwork(new Map([[asset.checksumsUrl, Uint8Array.of(0xff, 0xfe, 0x00)]]));
    await assert.rejects(installServer(asset, dir, fetchBytes), TypeError);
  });

  test("replaces binaries of other versions and keeps unrelated files", async () => {
    await mkdir(dir, { recursive: true });
    await writeFile(join(dir, "shell-lsp-v1.0.0-linux-amd64"), "old");
    await writeFile(join(dir, asset.name), "stale copy of the same version");
    await writeFile(join(dir, "unrelated"), "keep");
    const { fetchBytes } = fakeNetwork(
      new Map([
        [asset.checksumsUrl, sums({ [asset.name]: digest })],
        [asset.url, binary],
      ]),
    );
    await installServer(asset, dir, fetchBytes);
    assert.deepEqual((await readdir(dir)).sort(), [asset.name, "unrelated"]);
    assert.deepEqual(await readFile(join(dir, asset.name)), Buffer.from(binary));
  });
});
