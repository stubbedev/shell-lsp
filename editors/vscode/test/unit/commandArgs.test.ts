import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { referenceLocations, referenceTarget } from "../../src/commandArgs.ts";

const command = "shellLsp.showReferences";
const position = { line: 3, character: 7 };
const range = { start: { line: 1, character: 0 }, end: { line: 1, character: 5 } };

describe("referenceTarget", () => {
  test("reads the {uri, position} argument of a reference-count lens", () => {
    assert.deepEqual(referenceTarget(command, [{ uri: "file:///w/a.sh", position }]), {
      uri: "file:///w/a.sh",
      position,
    });
  });

  test("rejects anything else, naming the command", () => {
    for (const args of [
      [],
      [{ uri: "file:///w/a.sh" }],
      [{ uri: 1, position }],
      [{ uri: "file:///w/a.sh", position: { line: 1.5, character: 0 } }],
      [{ uri: "file:///w/a.sh", position }, "extra"],
      ["file:///w/a.sh"],
    ]) {
      assert.throws(() => referenceTarget(command, args), /^Error: shellLsp\.showReferences takes one \{uri, position\} argument/);
    }
  });
});

describe("referenceLocations", () => {
  test("accepts the locations the server answers", () => {
    const locations = [
      { uri: "file:///w/a.sh", range },
      { uri: "file:///w/b.sh", range },
    ];
    assert.deepEqual(referenceLocations(command, locations), locations);
    assert.deepEqual(referenceLocations(command, []), []);
  });

  test("rejects answers that are not locations", () => {
    for (const result of [null, {}, [{ uri: "file:///w/a.sh" }], [{ uri: "file:///w/a.sh", range: { start: position } }]]) {
      assert.throws(() => referenceLocations(command, result), /not a list of locations/);
    }
  });
});
