import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  expectedChecksum,
  isReleaseAssetName,
  parseChecksums,
  releaseAsset,
  releaseAssetProblems,
  serverDownload,
} from "../../src/release.ts";

const digestA = "a".repeat(64);
const digestB = "0123456789abcdef".repeat(4);

describe("serverDownload", () => {
  test("maps the Node platform and architecture to the Go names the release uses", () => {
    for (const [platform, arch, os, goArch] of [
      ["linux", "x64", "linux", "amd64"],
      ["linux", "arm64", "linux", "arm64"],
      ["darwin", "x64", "darwin", "amd64"],
      ["darwin", "arm64", "darwin", "arm64"],
      ["win32", "x64", "windows", "amd64"],
      ["win32", "arm64", "windows", "arm64"],
    ] as const) {
      assert.deepEqual(serverDownload("1.2.3", platform, arch), {
        kind: "release",
        asset: releaseAsset("1.2.3", { os, arch: goArch }),
      });
    }
  });

  test("names the platform that has no release binary", () => {
    assert.deepEqual(serverDownload("1.2.3", "win32", "ia32"), { kind: "unsupported", platform: "win32-ia32" });
    assert.deepEqual(serverDownload("1.2.3", "linux", "ia32"), { kind: "unsupported", platform: "linux-ia32" });
    assert.deepEqual(serverDownload("1.2.3", "freebsd", "x64"), { kind: "unsupported", platform: "freebsd-x64" });
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

  test("gives Windows assets the .exe suffix", () => {
    assert.equal(releaseAsset("1.2.3", { os: "windows", arch: "amd64" }).name, "shell-lsp-v1.2.3-windows-amd64.exe");
  });

  test("recognises release asset names of any version", () => {
    assert.ok(isReleaseAssetName(releaseAsset("0.9.0", { os: "linux", arch: "amd64" }).name));
    assert.ok(!isReleaseAssetName("shell-lsp"));
    assert.ok(!isReleaseAssetName("notes.txt"));
  });
});

describe("releaseAssetProblems", () => {
  const names = [
    "shell-lsp-v1.2.3-linux-amd64",
    "shell-lsp-v1.2.3-linux-arm64",
    "shell-lsp-v1.2.3-darwin-amd64",
    "shell-lsp-v1.2.3-darwin-arm64",
    "shell-lsp-v1.2.3-windows-amd64.exe",
    "shell-lsp-v1.2.3-windows-arm64.exe",
  ];
  const sumsOf = (files: readonly string[]): string => files.map((name) => `${digestA}  ${name}\n`).join("");

  test("accepts what just dist builds for every platform the extension downloads for", () => {
    assert.deepEqual(releaseAssetProblems("1.2.3", ["SHA256SUMS", ...names].reverse(), sumsOf(names)), []);
  });

  test("names a binary the extension downloads that was not built or summed", () => {
    const partial = names.filter((name) => !name.includes("windows-arm64"));
    assert.deepEqual(releaseAssetProblems("1.2.3", partial, sumsOf(partial)), [
      "the extension downloads shell-lsp-v1.2.3-windows-arm64.exe, which was not built",
      "the extension downloads shell-lsp-v1.2.3-windows-arm64.exe, which SHA256SUMS does not list",
    ]);
    assert.deepEqual(releaseAssetProblems("1.2.3", names, sumsOf(partial)), [
      "the extension downloads shell-lsp-v1.2.3-windows-arm64.exe, which SHA256SUMS does not list",
    ]);
  });

  test("names a binary that is built under a name the extension never asks for", () => {
    const renamed = names.map((name) => name.replace("darwin", "macos"));
    assert.deepEqual(releaseAssetProblems("1.2.3", renamed, sumsOf(names)), [
      "the extension downloads shell-lsp-v1.2.3-darwin-amd64, which was not built",
      "the extension downloads shell-lsp-v1.2.3-darwin-arm64, which was not built",
      "shell-lsp-v1.2.3-macos-amd64 was built, but the extension never downloads it",
      "shell-lsp-v1.2.3-macos-arm64 was built, but the extension never downloads it",
    ]);
    assert.equal(releaseAssetProblems("1.2.4", names, sumsOf(names)).length, 24);
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
