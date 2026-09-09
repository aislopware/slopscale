import type { TsConnectManifest, TsConnectUrls } from "./types.ts";
import type { IPN, IPNConfig } from "./wasm-js.d.ts";

export class TsConnectNotBuiltError extends Error {
  constructor(message = "Tailscale Connect client was not built (make wasm)") {
    super(message);
    this.name = "TsConnectNotBuiltError";
  }
}

export function buildTsConnectUrls(baseUrl: string = import.meta.env.BASE_URL): TsConnectUrls {
  const base = baseUrl.replace(/\/+$/u, "");
  return {
    manifestUrl: `${base}/tsconnect/manifest.json`,
    scriptUrl: (manifest: TsConnectManifest): string => `${base}/tsconnect/${manifest.execJs}`,
    wasmUrl: (manifest: TsConnectManifest): string => `${base}/tsconnect/${manifest.wasm}`,
  };
}

let scriptPromise: Promise<void> | null = null;
let initPromise: Promise<(config: IPNConfig) => IPN> | null = null;

interface ScriptSettlers {
  readonly resolve: () => void;
  readonly reject: (error: Error) => void;
}

/**
 * Settles the promise on the element's own outcome. A failed `<script>` never fires again, so it is
 * taken out of the document: the next attempt appends a fresh one instead of waiting forever on the
 * events of an element that is already done.
 */
function watchScript(
  script: HTMLScriptElement,
  scriptUrl: string,
  { resolve, reject }: ScriptSettlers,
): void {
  script.addEventListener(
    "load",
    (): void => {
      resolve();
    },
    { once: true },
  );
  script.addEventListener(
    "error",
    (): void => {
      script.remove();
      scriptPromise = null;
      reject(new Error(`Failed to load ${scriptUrl}`));
    },
    { once: true },
  );
}

export function loadWasmExecScript(scriptUrl: string): Promise<void> {
  if (typeof Go !== "undefined") {
    return Promise.resolve();
  }
  if (scriptPromise !== null) {
    return scriptPromise;
  }
  scriptPromise = new Promise<void>((resolve, reject) => {
    if (typeof document === "undefined") {
      reject(new Error("Document not available to inject script"));
      return;
    }
    const existing = document.querySelector<HTMLScriptElement>(`script[src="${scriptUrl}"]`);
    if (existing !== null) {
      if (typeof Go !== "undefined") {
        resolve();
        return;
      }
      watchScript(existing, scriptUrl, { resolve, reject });
      return;
    }
    const script = document.createElement("script");
    script.src = scriptUrl;
    script.async = true;
    watchScript(script, scriptUrl, { resolve, reject });
    document.head.append(script);
  });
  return scriptPromise;
}

const statusNotFound = 404;

function isManifest(value: unknown): value is TsConnectManifest {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  return (
    "wasm" in value &&
    typeof value.wasm === "string" &&
    "execJs" in value &&
    typeof value.execJs === "string"
  );
}

export async function fetchManifest(manifestUrl: string): Promise<TsConnectManifest> {
  const res = await fetch(manifestUrl);
  if (res.status === statusNotFound) {
    throw new TsConnectNotBuiltError();
  }
  if (!res.ok) {
    throw new Error(`Failed to load Tailscale Connect manifest: ${res.status} ${res.statusText}`);
  }
  const data: unknown = await res.json();
  if (!isManifest(data)) {
    throw new TypeError("Invalid Tailscale Connect manifest");
  }
  return data;
}

async function instantiateWasm(
  wasmUrl: string,
  importObject: WebAssembly.Imports,
): Promise<WebAssembly.WebAssemblyInstantiatedSource> {
  try {
    return await WebAssembly.instantiateStreaming(fetch(wasmUrl), importObject);
  } catch {
    const resp = await fetch(wasmUrl);
    const bytes = await resp.arrayBuffer();
    return WebAssembly.instantiate(bytes, importObject);
  }
}

async function runGoProcess(
  go: Go,
  instance: WebAssembly.Instance,
  onPanic?: (error: string) => void,
): Promise<void> {
  try {
    await go.run(instance);
    onPanic?.("Tailscale client unexpectedly shut down");
  } catch {
    onPanic?.("Tailscale client runtime error");
  }
}

async function loadAndInitWasm(
  baseUrl: string,
  onPanic?: (error: string) => void,
): Promise<(config: IPNConfig) => IPN> {
  const urls = buildTsConnectUrls(baseUrl);
  const manifest = await fetchManifest(urls.manifestUrl);
  await loadWasmExecScript(urls.scriptUrl(manifest));

  if (typeof newIPN === "function") {
    return newIPN;
  }

  const go = new Go();
  const wasmSource = await instantiateWasm(urls.wasmUrl(manifest), go.importObject);

  void runGoProcess(go, wasmSource.instance, onPanic);

  if (typeof newIPN !== "function") {
    throw new TypeError("Tailscale wasm initialized but newIPN was not registered");
  }

  return newIPN;
}

async function loadTsConnectWithCache(
  baseUrl: string,
  onPanic?: (error: string) => void,
): Promise<(config: IPNConfig) => IPN> {
  try {
    return await loadAndInitWasm(baseUrl, onPanic);
  } catch (error: unknown) {
    initPromise = null;
    throw error;
  }
}

export function loadTsConnect(
  baseUrl: string = import.meta.env.BASE_URL,
  onPanic?: (error: string) => void,
): Promise<(config: IPNConfig) => IPN> {
  if (typeof newIPN === "function") {
    return Promise.resolve(newIPN);
  }
  if (initPromise !== null) {
    return initPromise;
  }

  initPromise = loadTsConnectWithCache(baseUrl, onPanic);
  return initPromise;
}
