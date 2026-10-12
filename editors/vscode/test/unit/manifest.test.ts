import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { describe, test } from "node:test";
import { isFields, type Fields } from "../../src/fields.ts";
import { clientCommands, runningContext, serverCommands } from "../../src/ids.ts";

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
