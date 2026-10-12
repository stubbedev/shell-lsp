import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { describe, test } from "node:test";
import { languageIds } from "../../src/documents.ts";
import { isFields, type Fields } from "../../src/fields.ts";
import { clientCommands, runningContext, serverCommands } from "../../src/ids.ts";
import { parseManifest } from "../../src/manifest.ts";

const manifest: unknown = JSON.parse(await readFile(join(import.meta.dirname, "..", "..", "package.json"), "utf8"));

function field(value: unknown, path: readonly string[]): unknown {
  return path.reduce<unknown>((current, key) => {
    if (!isFields(current)) {
      assert.fail(`package.json has no object at ${path.join(".")}`);
    }
    return current[key];
  }, value);
}

function entries(path: readonly string[]): readonly Fields[] {
  const list = field(manifest, path);
  assert.ok(Array.isArray(list) && list.every(isFields), `package.json ${path.join(".")} is not a list of objects`);
  return list;
}

function stringOf(entry: Fields, key: string): string {
  const value = entry[key];
  assert.equal(typeof value, "string", `${JSON.stringify(entry)} has no string ${key}`);
  return String(value);
}

const contributed = entries(["contributes", "commands"]).map((entry) => stringOf(entry, "command"));
const palette = entries(["contributes", "menus", "commandPalette"]);

describe("activationEvents", () => {
  test("start the extension for every language the client attaches to", () => {
    assert.deepEqual(
      field(manifest, ["activationEvents"]),
      languageIds.map((id) => `onLanguage:${id}`),
    );
  });
});

describe("contributes.commands", () => {
  test("lists every command the extension registers", () => {
    for (const command of Object.values(clientCommands)) {
      assert.ok(contributed.includes(command), `${command} is registered but not contributed`);
    }
  });

  test("lists only commands the extension or the server provides, once each", () => {
    const known: readonly string[] = [...Object.values(clientCommands), ...Object.values(serverCommands)];
    for (const command of contributed) {
      assert.ok(known.includes(command), `${command} is contributed but neither the extension nor the server provides it`);
    }
    assert.equal(new Set(contributed).size, contributed.length, `a command is contributed twice: ${contributed.join(", ")}`);
  });
});

describe("contributes.menus.commandPalette", () => {
  test("offers each command in every language the client attaches to", () => {
    const languages = `(${languageIds.map((id) => `editorLangId == ${id}`).join(" || ")})`;
    for (const entry of palette) {
      assert.ok(stringOf(entry, "when").endsWith(` && ${languages}`), `${stringOf(entry, "command")} is not offered for ${languages}`);
    }
  });

  test("names contributed commands and hides them until the server runs", () => {
    for (const entry of palette) {
      const command = stringOf(entry, "command");
      assert.ok(contributed.includes(command), `the palette lists ${command}, which is not contributed`);
      assert.ok(
        stringOf(entry, "when").startsWith(`${runningContext} && `),
        `${command} is offered before the server runs: ${stringOf(entry, "when")}`,
      );
    }
  });
});

describe("parseManifest", () => {
  const configuration = (properties: Fields): Fields => ({ contributes: { configuration: [{ title: "A", properties }] } });
  const resource = { scope: "resource" };

  test("lists the contributed settings the server reads, without the extension's own", () => {
    const keys = parseManifest(manifest).serverKeys;
    assert.ok(keys.includes("dialect"));
    assert.ok(keys.includes("lint.exclude"));
    assert.ok(keys.includes("format.binaryNextLine"));
    assert.ok(!keys.includes("path"));
    assert.ok(!keys.includes("trace.server"));
  });

  test("reads every configuration category and ignores other extensions' keys", () => {
    const parsed = parseManifest({
      version: "1.2.3",
      contributes: {
        configuration: [
          { title: "A", properties: { "shellLsp.path": { scope: "machine" }, "shellLsp.dialect": resource, "editor.tabSize": resource } },
          { title: "B", properties: { "shellLsp.trace.server": { scope: "window" }, "shellLsp.lint.enable": resource } },
        ],
      },
    });
    assert.deepEqual(parsed, { version: "1.2.3", serverKeys: ["dialect", "lint.enable"] });
  });

  test("leaves out settings that cannot differ per workspace folder, scoped or not", () => {
    const parsed = parseManifest({
      version: "1.2.3",
      ...configuration({ "shellLsp.a": { scope: "application" }, "shellLsp.b": {}, "shellLsp.c": resource }),
    });
    assert.deepEqual(parsed.serverKeys, ["c"]);
  });

  test("rejects a manifest without configuration categories", () => {
    assert.throws(() => parseManifest({ version: "1.2.3" }), /no contributes\.configuration list/);
    assert.throws(() => parseManifest({ version: "1.2.3", contributes: { configuration: [{ title: "x" }] } }), /has no properties/);
  });

  test("reads a release version", () => {
    assert.equal(parseManifest({ version: "1.2.3", ...configuration({}) }).version, "1.2.3");
    assert.equal(parseManifest({ version: "1.2.3-rc.1", ...configuration({}) }).version, "1.2.3-rc.1");
  });

  test("rejects manifests without a usable version", () => {
    for (const version of [undefined, 3, "v1.2.3", "1.2", "1.2.3/../x"]) {
      assert.throws(() => parseManifest({ version, ...configuration({}) }), /is not a release version/);
    }
    assert.throws(() => parseManifest(null), /the extension manifest is null, not an object/);
  });
});
