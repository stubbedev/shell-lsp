import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { describe, test } from "node:test";
import { explicitSettings, serverKeys, type ScopedValues } from "../../src/settings.ts";

describe("serverKeys", () => {
  test("lists the contributed settings the server reads, without the extension's own", async () => {
    const manifest: unknown = JSON.parse(await readFile(join(import.meta.dirname, "..", "..", "package.json"), "utf8"));
    const keys = serverKeys(manifest);
    assert.ok(keys.includes("dialect"));
    assert.ok(keys.includes("lint.exclude"));
    assert.ok(keys.includes("format.binaryNextLine"));
    assert.ok(!keys.includes("path"));
    assert.ok(!keys.includes("trace.server"));
  });

  test("reads every configuration category and ignores other extensions' keys", () => {
    const resource = { scope: "resource" };
    const manifest = {
      contributes: {
        configuration: [
          { title: "A", properties: { "shellLsp.path": { scope: "machine" }, "shellLsp.dialect": resource, "editor.tabSize": resource } },
          { title: "B", properties: { "shellLsp.trace.server": { scope: "window" }, "shellLsp.lint.enable": resource } },
        ],
      },
    };
    assert.deepEqual(serverKeys(manifest), ["dialect", "lint.enable"]);
  });

  test("leaves out settings that cannot differ per workspace folder, scoped or not", () => {
    const manifest = {
      contributes: {
        configuration: [{ title: "A", properties: { "shellLsp.a": { scope: "application" }, "shellLsp.b": {}, "shellLsp.c": { scope: "resource" } } }],
      },
    };
    assert.deepEqual(serverKeys(manifest), ["c"]);
  });

  test("rejects a manifest without configuration categories", () => {
    assert.throws(() => serverKeys({}), /no contributes\.configuration list/);
    assert.throws(() => serverKeys({ contributes: { configuration: [{ title: "x" }] } }), /has no properties/);
  });
});

describe("explicitSettings", () => {
  const inspector =
    (values: Readonly<Record<string, ScopedValues>>) =>
    (key: string): ScopedValues | undefined =>
      values[key];

  test("sends only values the user set, so server and .editorconfig defaults apply", () => {
    const settings = explicitSettings(
      ["dialect", "lint.enable", "lint.exclude", "format.binaryNextLine", "format.indent"],
      inspector({
        "lint.exclude": { globalValue: ["SC2034"] },
        "format.binaryNextLine": {},
      }),
    );
    assert.deepEqual(settings, { lint: { exclude: ["SC2034"] } });
  });

  test("prefers the folder over the workspace over the user value", () => {
    const settings = explicitSettings(
      ["dialect", "format.indent", "lint.enable"],
      inspector({
        dialect: { globalValue: "bash", workspaceValue: "zsh", workspaceFolderValue: "ksh" },
        "format.indent": { globalValue: 4, workspaceValue: 2 },
        "lint.enable": { globalValue: false },
      }),
    );
    assert.deepEqual(settings, { dialect: "ksh", format: { indent: 2 }, lint: { enable: false } });
  });

  test("keeps an explicit null that overrides a broader scope", () => {
    const settings = explicitSettings(["dialect"], inspector({ dialect: { globalValue: "zsh", workspaceValue: null } }));
    assert.deepEqual(settings, { dialect: null });
  });

  test("groups nested keys under one object", () => {
    const settings = explicitSettings(
      ["format.indent", "format.minify", "lint.optional"],
      inspector({
        "format.indent": { globalValue: 0 },
        "format.minify": { globalValue: false },
        "lint.optional": { workspaceValue: ["all"] },
      }),
    );
    assert.deepEqual(settings, { format: { indent: 0, minify: false }, lint: { optional: ["all"] } });
  });
});
