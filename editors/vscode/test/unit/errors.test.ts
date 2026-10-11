import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { describeError } from "../../src/errors.ts";

describe("describeError", () => {
  test("joins the messages of the whole cause chain", () => {
    const error = new Error("downloading https://x/y", { cause: new Error("HTTP 404 Not Found") });
    assert.equal(describeError(new Error("installing", { cause: error })), "installing: downloading https://x/y: HTTP 404 Not Found");
  });

  test("describes values that are not errors", () => {
    assert.equal(describeError("plain"), "plain");
    assert.equal(describeError(new Error("outer", { cause: 42 })), "outer: 42");
  });
});
