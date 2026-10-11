import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { after, before, describe, test } from "node:test";
import { httpFetcher } from "../../src/http.ts";

const payload = Buffer.alloc(4096, 7);

async function failure(promise: Promise<unknown>): Promise<Error> {
  try {
    await promise;
  } catch (error) {
    assert.ok(error instanceof Error);
    return error;
  }
  assert.fail("expected the download to fail");
}

function causeOf(error: Error): Error {
  assert.ok(error.cause instanceof Error);
  return error.cause;
}

describe("httpFetcher", () => {
  let server: Server | undefined;
  let base = "";

  before(async () => {
    const listening = createServer((request, response) => {
      switch (request.url) {
        case "/asset":
          response.end(payload);
          return;
        case "/redirect":
          response.writeHead(302, { location: "/asset" }).end();
          return;
        case "/chunked":
          response.write(payload);
          response.end(payload);
          return;
        case "/stall":
          return;
        default:
          response.writeHead(404, "Not Found").end();
      }
    });
    server = listening;
    await new Promise<void>((resolve) => listening.listen(0, "127.0.0.1", resolve));
    base = `http://127.0.0.1:${String((listening.address() as AddressInfo).port)}`;
  });

  after(async () => {
    server?.closeAllConnections();
    await new Promise((resolve) => server?.close(resolve));
  });

  const generous = { timeoutMs: 5_000, maxBytes: 1 << 20 };

  test("returns the body of a successful response", async () => {
    assert.deepEqual(Buffer.from(await httpFetcher(generous)(`${base}/asset`)), payload);
  });

  test("follows redirects", async () => {
    assert.deepEqual(Buffer.from(await httpFetcher(generous)(`${base}/redirect`)), payload);
  });

  test("fails with the URL and status of an unsuccessful response", async () => {
    const error = await failure(httpFetcher(generous)(`${base}/missing`));
    assert.equal(error.message, `downloading ${base}/missing`);
    assert.equal(causeOf(error).message, "HTTP 404 Not Found");
  });

  test("refuses a response whose declared length exceeds the limit", async () => {
    const error = await failure(httpFetcher({ ...generous, maxBytes: 100 })(`${base}/asset`));
    assert.equal(causeOf(error).message, `the response is ${String(payload.length)} bytes, more than the 100 allowed`);
  });

  test("stops reading a streamed response once it exceeds the limit", async () => {
    const limit = payload.length + 1;
    const error = await failure(httpFetcher({ ...generous, maxBytes: limit })(`${base}/chunked`));
    assert.equal(causeOf(error).message, `the response exceeds the ${String(limit)} bytes allowed`);
  });

  test("gives up when the server does not answer in time", async () => {
    const error = await failure(httpFetcher({ ...generous, timeoutMs: 100 })(`${base}/stall`));
    assert.equal(error.message, `downloading ${base}/stall`);
    assert.equal(causeOf(error).name, "TimeoutError");
  });
});
