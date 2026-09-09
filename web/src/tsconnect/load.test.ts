import { describe, expect, it, vi } from "vitest";

import {
  buildTsConnectUrls,
  fetchManifest,
  loadWasmExecScript,
  TsConnectNotBuiltError,
} from "~/tsconnect/load.ts";

const manifestUrl = "/admin/tsconnect/manifest.json";
const statusNotFound = 404;
const statusServerError = 500;

/**
 * Answers the next fetch with one canned response. `unstubGlobals` in the vitest config puts the
 * real fetch back after each test, so this needs no teardown.
 */
function respondWith(body: unknown, status = 200): void {
  vi.stubGlobal(
    "fetch",
    vi.fn((): Promise<Response> => Promise.resolve(Response.json(body, { status }))),
  );
}

describe(buildTsConnectUrls, () => {
  const manifest = { wasm: "main-hash123.wasm", execJs: "wasm_exec.js" };

  it("puts the client next to the console's base path", () => {
    const urls = buildTsConnectUrls("/admin/");

    expect(urls.manifestUrl).toBe(manifestUrl);
    expect(urls.scriptUrl(manifest)).toBe("/admin/tsconnect/wasm_exec.js");
    expect(urls.wasmUrl(manifest)).toBe("/admin/tsconnect/main-hash123.wasm");
  });

  it("does not care whether the base path ends in a slash", () => {
    expect(buildTsConnectUrls("/admin").manifestUrl).toBe(manifestUrl);
    expect(buildTsConnectUrls("/admin//").manifestUrl).toBe(manifestUrl);
  });

  it("serves from the root when the console is mounted there", () => {
    expect(buildTsConnectUrls("").manifestUrl).toBe("/tsconnect/manifest.json");
    expect(buildTsConnectUrls("/").manifestUrl).toBe("/tsconnect/manifest.json");
  });
});

describe(fetchManifest, () => {
  it("returns the manifest the build wrote", async () => {
    respondWith({ wasm: "main-abc.wasm", execJs: "wasm_exec.js", sha256: "abc" });

    await expect(fetchManifest(manifestUrl)).resolves.toStrictEqual({
      wasm: "main-abc.wasm",
      execJs: "wasm_exec.js",
      sha256: "abc",
    });
  });

  it("reads a missing manifest as a client that was never built", async () => {
    respondWith({}, statusNotFound);

    await expect(fetchManifest(manifestUrl)).rejects.toBeInstanceOf(TsConnectNotBuiltError);
  });

  it("rejects a manifest without the file names", async () => {
    respondWith({ execJs: "wasm_exec.js" });

    await expect(fetchManifest(manifestUrl)).rejects.toBeInstanceOf(TypeError);
  });

  it("reports any other failure with its status", async () => {
    respondWith({}, statusServerError);

    await expect(fetchManifest(manifestUrl)).rejects.toThrow("500");
  });
});

const scriptUrl = "/tsconnect/wasm-exec-under-test.js";

/** The element the loader appended for `scriptUrl`, which the test settles by hand. */
function injectedScript(): HTMLScriptElement {
  const script = document.querySelector<HTMLScriptElement>(`script[src="${scriptUrl}"]`);
  if (script === null) {
    throw new Error(`No script was appended for ${scriptUrl}`);
  }
  return script;
}

describe(loadWasmExecScript, () => {
  it("drops the failed element so a second load appends a fresh one", async () => {
    const first = loadWasmExecScript(scriptUrl);
    const failing = injectedScript();
    failing.dispatchEvent(new Event("error"));

    await expect(first).rejects.toThrow(`Failed to load ${scriptUrl}`);
    expect(document.querySelector(`script[src="${scriptUrl}"]`)).toBeNull();

    const second = loadWasmExecScript(scriptUrl);
    const retry = injectedScript();
    expect(retry).not.toBe(failing);
    retry.dispatchEvent(new Event("load"));

    await expect(second).resolves.toBeUndefined();
    retry.remove();
  });
});
