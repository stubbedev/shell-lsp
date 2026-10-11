import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  expectedChecksum,
  extensionVersion,
  isReleaseAssetName,
  parseChecksums,
  releaseAsset,
  releaseTarget,
} from "../../src/release.ts";

const digestA = "a".repeat(64);
const digestB = "0123456789abcdef".repeat(4);

describe("releaseTarget", () => {
  test("maps the Node platform and architecture to the Go names the release uses", () => {
    assert.deepEqual(releaseTarget("linux", "x64"), { os: "linux", arch: "amd64" });
    assert.deepEqual(releaseTarget("linux", "arm64"), { os: "linux", arch: "arm64" });
    assert.deepEqual(releaseTarget("darwin", "x64"), { os: "darwin", arch: "amd64" });
    assert.deepEqual(releaseTarget("darwin", "arm64"), { os: "darwin", arch: "arm64" });
  });

  test("has no target for platforms without release binaries", () => {
    assert.equal(releaseTarget("win32", "x64"), undefined);
    assert.equal(releaseTarget("linux", "ia32"), undefined);
    assert.equal(releaseTarget("freebsd", "x64"), undefined);
  });
});

describe("releaseAsset", () => {
  test("names the asset and checksum file of the release tagged with the version", () => {
    assert.deepEqual(releaseAsset("1.2.3", { os: "darwin", arch: "arm64" }), {
      name: "shell-lsp-v1.2.3-darwin-arm64",
      url: "https://github.com/stubbedev/shell-lsp/releases/download/v1.2.3/shell-lsp-v1.2.3-darwin-arm64",
      checksumsUrl: "https://github.com/stubbedev/shell-lsp/releases/download/v1.2.3/SHA256SUMS",
    });
  });

  test("recognises release asset names of any version", () => {
    assert.ok(isReleaseAssetName(releaseAsset("0.9.0", { os: "linux", arch: "amd64" }).name));
    assert.ok(!isReleaseAssetName("shell-lsp"));
    assert.ok(!isReleaseAssetName("notes.txt"));
  });
});

describe("extensionVersion", () => {
  test("reads a release version from the manifest", () => {
    assert.equal(extensionVersion({ version: "1.2.3" }), "1.2.3");
    assert.equal(extensionVersion({ version: "1.2.3-rc.1" }), "1.2.3-rc.1");
  });

  test("rejects manifests without a usable version", () => {
    for (const manifest of [{}, { version: 3 }, { version: "v1.2.3" }, { version: "1.2" }, { version: "1.2.3/../x" }, null]) {
      assert.throws(() => extensionVersion(manifest), /is not a release version/);
    }
  });
});

describe("parseChecksums", () => {
  test("reads sha256sum output in text and binary mode", () => {
    const sums = parseChecksums(`${digestA}  shell-lsp-v1.0.0-linux-amd64\n${digestB} *shell-lsp-v1.0.0-linux-arm64\n\n`);
    assert.deepEqual(
      [...sums],
      [
        ["shell-lsp-v1.0.0-linux-amd64", digestA],
        ["shell-lsp-v1.0.0-linux-arm64", digestB],
      ],
    );
  });

  test("rejects malformed lines with their line number", () => {
    assert.throws(() => parseChecksums(`${digestA}  ok\nnot a checksum\n`), /line 2 /);
    assert.throws(() => parseChecksums(`${digestA.toUpperCase()}  upper\n`), /line 1 /);
    assert.throws(() => parseChecksums(`${digestA.slice(1)}  short\n`), /line 1 /);
  });

  test("rejects files listed twice", () => {
    assert.throws(() => parseChecksums(`${digestA}  twice\n${digestB}  twice\n`), /twice more than once/);
  });
});

describe("expectedChecksum", () => {
  test("returns the digest listed for the asset", () => {
    assert.equal(expectedChecksum(`${digestA}  one\n${digestB}  two\n`, "two"), digestB);
  });

  test("fails when the asset is not listed", () => {
    assert.throws(() => expectedChecksum(`${digestA}  one\n`, "two"), /no entry for two/);
  });
});
