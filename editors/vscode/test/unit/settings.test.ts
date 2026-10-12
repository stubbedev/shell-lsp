import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { explicitSettings, type ScopedValues } from "../../src/settings.ts";

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
