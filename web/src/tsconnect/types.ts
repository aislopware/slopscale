import type { IPNState } from "./wasm-js.d.ts";

export interface TsConnectManifest {
  readonly wasm: string;
  readonly execJs: string;
  readonly sha256?: string;
}

export interface TsConnectUrls {
  readonly manifestUrl: string;
  readonly scriptUrl: (manifest: TsConnectManifest) => string;
  readonly wasmUrl: (manifest: TsConnectManifest) => string;
}

export type SSHStatus =
  | "loading-client"
  | "joining-tailnet"
  | "connecting"
  | "connected"
  | "done"
  | "error";

export interface SSHSessionState {
  readonly status: SSHStatus;
  readonly ipnState: IPNState;
  readonly progressMessage: string | null;
  readonly error: string | null;
  readonly notBuilt: boolean;
}

export type SSHAction =
  | { readonly type: "START_LOADING" }
  | { readonly type: "CLIENT_LOADED" }
  | { readonly type: "NOTIFY_IPN_STATE"; readonly ipnState: IPNState }
  | { readonly type: "START_CONNECTING" }
  | { readonly type: "CONNECTION_PROGRESS"; readonly message: string }
  | { readonly type: "CONNECTED" }
  | { readonly type: "DONE" }
  | { readonly type: "ERROR"; readonly error: string; readonly notBuilt?: boolean }
  | { readonly type: "RECONNECT" };

export interface SSHSessionDef {
  readonly host: string;
  readonly username: string;
  readonly timeoutSeconds?: number;
}
