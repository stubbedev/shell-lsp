import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import { after, before, describe, test } from "node:test";
import { findServer, isExecutableFile, type Lookup } from "../../src/serverBinary.ts";

function probeOf(executables: readonly string[]): { probe: (path: string) => Promise<boolean>; probed: string[] } {
  const probed: string[] = [];
  return {
    probed,
    probe: (path) => {
      probed.push(path);
      return Promise.resolve(executables.includes(path));
    },
  };
}

const none: Lookup = { platform: "linux", configuredPath: "", pathEnv: undefined, downloaded: undefined };

describe("findServer", () => {
  test("uses the configured path without searching PATH", async () => {
    const { probe, probed } = probeOf(["/opt/shell-lsp", "/usr/bin/shell-lsp"]);
    const found = await findServer({ ...none, configuredPath: "/opt/shell-lsp", pathEnv: "/usr/bin" }, probe);
    assert.deepEqual(found, { path: "/opt/shell-lsp", source: "setting" });
    assert.deepEqual(probed, ["/opt/shell-lsp"]);
  });

  test("fails instead of falling back when the configured path is not executable", async () => {
    const { probe } = probeOf(["/usr/bin/shell-lsp"]);
    await assert.rejects(
      findServer({ ...none, configuredPath: "/opt/missing", pathEnv: "/usr/bin" }, probe),
      /shellLsp\.path is \/opt\/missing, which is not an executable file/,
    );
  });

  test("rejects a relative configured path", async () => {
    const { probe, probed } = probeOf(["bin/shell-lsp"]);
    await assert.rejects(findServer({ ...none, configuredPath: "bin/shell-lsp" }, probe), /must be an absolute path/);
    assert.deepEqual(probed, []);
  });

  test("takes the first PATH entry that holds an executable shell-lsp", async () => {
    const { probe, probed } = probeOf(["/b/shell-lsp", "/c/shell-lsp"]);
    const found = await findServer({ ...none, pathEnv: ["/a", "/b", "/c"].join(delimiter) }, probe);
    assert.deepEqual(found, { path: "/b/shell-lsp", source: "PATH" });
    assert.deepEqual(probed, ["/a/shell-lsp", "/b/shell-lsp"]);
  });

  test("ignores empty and relative PATH entries", async () => {
    const { probe, probed } = probeOf(["shell-lsp", "bin/shell-lsp"]);
    const found = await findServer({ ...none, pathEnv: ["", ".", "bin", "/z"].join(delimiter) }, probe);
    assert.equal(found, undefined);
    assert.deepEqual(probed, ["/z/shell-lsp"]);
  });

  test("prefers PATH over a downloaded binary", async () => {
    const { probe } = probeOf(["/usr/bin/shell-lsp", "/storage/shell-lsp-v1.0.0-linux-amd64"]);
    const found = await findServer(
      { ...none, pathEnv: "/usr/bin", downloaded: "/storage/shell-lsp-v1.0.0-linux-amd64" },
      probe,
    );
    assert.deepEqual(found, { path: "/usr/bin/shell-lsp", source: "PATH" });
  });

  test("falls back to the downloaded binary when PATH has none", async () => {
    const { probe } = probeOf(["/storage/shell-lsp-v1.0.0-linux-amd64"]);
    const found = await findServer(
      { ...none, pathEnv: "/usr/bin", downloaded: "/storage/shell-lsp-v1.0.0-linux-amd64" },
      probe,
    );
    assert.deepEqual(found, { path: "/storage/shell-lsp-v1.0.0-linux-amd64", source: "download" });
  });

  test("looks for shell-lsp.exe in Windows PATH entries", async () => {
    const { probe, probed } = probeOf(["C:\\tools\\shell-lsp.exe"]);
    const found = await findServer({ ...none, platform: "win32", pathEnv: "relative;C:\\bin;C:\\tools" }, probe);
    assert.deepEqual(found, { path: "C:\\tools\\shell-lsp.exe", source: "PATH" });
    assert.deepEqual(probed, ["C:\\bin\\shell-lsp.exe", "C:\\tools\\shell-lsp.exe"]);
  });

  test("accepts an absolute Windows path in the setting", async () => {
    const { probe } = probeOf(["D:\\shell-lsp.exe"]);
    const found = await findServer({ ...none, platform: "win32", configuredPath: "D:\\shell-lsp.exe" }, probe);
    assert.deepEqual(found, { path: "D:\\shell-lsp.exe", source: "setting" });
  });

  test("finds nothing when no candidate is executable", async () => {
    const { probe } = probeOf([]);
    assert.equal(await findServer({ ...none, pathEnv: "/usr/bin", downloaded: "/storage/x" }, probe), undefined);
  });
});

describe("isExecutableFile", () => {
  let dir = "";

  before(async () => {
    dir = await mkdtemp(join(tmpdir(), "shell-lsp-probe-"));
    await writeFile(join(dir, "exec"), "#!/bin/sh\n", { mode: 0o755 });
    await writeFile(join(dir, "plain"), "data", { mode: 0o644 });
    await mkdir(join(dir, "dir"));
    await chmod(join(dir, "dir"), 0o755);
  });

  after(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  test("accepts an executable regular file", async () => {
    assert.equal(await isExecutableFile(join(dir, "exec")), true);
  });

  test("rejects files without the execute bit, directories and missing paths", async () => {
    assert.equal(await isExecutableFile(join(dir, "plain")), false);
    assert.equal(await isExecutableFile(join(dir, "dir")), false);
    assert.equal(await isExecutableFile(join(dir, "missing")), false);
    assert.equal(await isExecutableFile(join(dir, "plain", "below")), false);
  });
});
