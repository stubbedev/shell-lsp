import type { FetchBytes } from "./install.ts";

export interface FetchLimits {
  readonly timeoutMs: number;
  readonly maxBytes: number;
}

export function httpFetcher(limits: FetchLimits): FetchBytes {
  return async (url) => {
    try {
      return await fetchLimited(url, limits);
    } catch (error) {
      throw new Error(`downloading ${url}`, { cause: error });
    }
  };
}

async function fetchLimited(url: string, limits: FetchLimits): Promise<Uint8Array> {
  const response = await fetch(url, { signal: AbortSignal.timeout(limits.timeoutMs) });
  if (!response.ok) {
    throw new Error(`HTTP ${String(response.status)} ${response.statusText}`);
  }
  if (response.body === null) {
    throw new Error("the response has no body");
  }
  const declared = Number(response.headers.get("content-length") ?? "0");
  if (declared > limits.maxBytes) {
    throw new Error(`the response is ${String(declared)} bytes, more than the ${String(limits.maxBytes)} allowed`);
  }
  const chunks: Uint8Array[] = [];
  let size = 0;
  const reader: ReadableStreamDefaultReader<Uint8Array> = response.body.getReader();
  for (let read = await reader.read(); !read.done; read = await reader.read()) {
    size += read.value.byteLength;
    if (size > limits.maxBytes) {
      await reader.cancel();
      throw new Error(`the response exceeds the ${String(limits.maxBytes)} bytes allowed`);
    }
    chunks.push(read.value);
  }
  return Buffer.concat(chunks, size);
}
