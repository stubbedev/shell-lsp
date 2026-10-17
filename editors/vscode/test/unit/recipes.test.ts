import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { argumentsFrom, needsPrompt, promptFor, recipeListings, recipeRun, runsWithoutArguments } from "../../src/recipes.ts";

const command = "shellLsp.runRecipe";
const name = { name: "name", optional: false, variadic: false };
const level = { name: "level", default: '"1"', optional: true, variadic: false };
const rest = { name: "rest", optional: true, variadic: true };

describe("recipeRun", () => {
  test("reads the argument of a run lens", () => {
    assert.deepEqual(recipeRun(command, [{ uri: "file:///w/justfile", name: "build" }]), { uri: "file:///w/justfile", name: "build" });
    assert.deepEqual(recipeRun(command, [{ uri: "file:///w/justfile", name: "with-arg", parameters: [name, level] }]), {
      uri: "file:///w/justfile",
      name: "with-arg",
      parameters: [name, level],
    });
  });

  test("rejects anything else, naming the command", () => {
    for (const args of [[], [{ uri: "file:///w/justfile" }], [{ uri: "file:///w/justfile", name: "a", arguments: [1] }], [{ uri: "u", name: "a", parameters: [{ name: "x" }] }]]) {
      assert.throws(() => recipeRun(command, args), /^Error: shellLsp\.runRecipe takes one \{uri, name, arguments\?, parameters\?\} argument/);
    }
  });
});

describe("needsPrompt", () => {
  test("prompts only when the lens lists parameters and gives no arguments", () => {
    assert.equal(needsPrompt({ uri: "u", name: "a" }), false);
    assert.equal(needsPrompt({ uri: "u", name: "a", parameters: [name] }), true);
    assert.equal(needsPrompt({ uri: "u", name: "a", parameters: [name], arguments: ["x"] }), false);
  });
});

describe("argumentsFrom", () => {
  test("turns answers into arguments, dropping empty optional ones and splitting variadic ones", () => {
    assert.deepEqual(argumentsFrom([name, level, rest], ["world", "", "a  b"]), ["world", "a", "b"]);
    assert.deepEqual(argumentsFrom([name, level], ["world", "2"]), ["world", "2"]);
    assert.deepEqual(argumentsFrom([name], [""]), [""]);
  });
});

describe("promptFor", () => {
  test("names the parameter with its default and variadic hint", () => {
    assert.equal(promptFor(name), "name");
    assert.equal(promptFor(level), 'level (default "1")');
    assert.equal(promptFor(rest), "rest (separate several with spaces)");
  });
});

describe("recipeListings", () => {
  const listing = { uri: "file:///w/justfile", path: "/w/justfile", name: "build", tool: "just", argv: ["just", "build"], cwd: "/w", parameters: [] };

  test("accepts the recipes the server lists", () => {
    assert.deepEqual(recipeListings("shellLsp.listRecipes", [listing]), [listing]);
    assert.equal(runsWithoutArguments(listing), true);
    assert.equal(runsWithoutArguments({ ...listing, parameters: [name] }), false);
  });

  test("rejects other answers", () => {
    assert.throws(() => recipeListings("shellLsp.listRecipes", [{ ...listing, argv: [] }]), /shellLsp\.listRecipes answered/);
  });
});
