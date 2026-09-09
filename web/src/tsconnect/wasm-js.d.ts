// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

/**
 * @file Type definitions for types exported by the wasm_js.go Go
 * module.
 */

export interface Go {
  importObject: WebAssembly.Imports;
  run: (instance: WebAssembly.Instance) => Promise<void>;
}

export interface IPNSSHSession {
  readonly resize: (rows: number, cols: number) => boolean;
  readonly close: () => boolean;
}

export interface IPNStateStorage {
  readonly setState: (id: string, value: string) => void;
  readonly getState: (id: string) => string;
}

export interface IPNConfig {
  stateStorage?: IPNStateStorage;
  authKey?: string;
  controlURL?: string;
  hostname?: string;
}

export interface IPNCallbacks {
  readonly notifyState: (state: IPNState) => void;
  readonly notifyNetMap: (netMapStr: string) => void;
  readonly notifyBrowseToURL: (url: string) => void;
  readonly notifyPanicRecover: (error: string) => void;
}

export interface IPN {
  readonly run: (callbacks: IPNCallbacks) => void;
  readonly login: () => void;
  readonly logout: () => void;
  readonly ssh: (
    host: string,
    username: string,
    termConfig: {
      writeFn: (data: string) => void;
      writeErrorFn: (error: string) => void;
      setReadFn: (readFn: (data: string) => void) => void;
      rows: number;
      cols: number;
      /** Defaults to 5 seconds */
      timeoutSeconds?: number;
      onConnectionProgress: (message: string) => void;
      onConnected: () => void;
      onDone: () => void;
    },
  ) => IPNSSHSession;
  readonly fetch: (url: string) => Promise<{
    status: number;
    statusText: string;
    text: () => Promise<string>;
  }>;
}

export type IPNState =
  | "NoState"
  | "InUseOtherUser"
  | "NeedsLogin"
  | "NeedsMachineAuth"
  | "Stopped"
  | "Starting"
  | "Running";

export type IPNMachineStatus =
  | "MachineUnknown"
  | "MachineUnauthorized"
  | "MachineAuthorized"
  | "MachineInvalid";

export interface IPNNetMapNode {
  readonly name: string;
  readonly addresses: readonly string[];
  readonly machineKey: string;
  readonly nodeKey: string;
}

export interface IPNNetMapSelfNode extends IPNNetMapNode {
  readonly machineStatus: IPNMachineStatus;
}

export interface IPNNetMapPeerNode extends IPNNetMapNode {
  readonly online?: boolean;
  readonly tailscaleSSHEnabled: boolean;
}

export interface IPNNetMap {
  readonly self: IPNNetMapSelfNode;
  readonly peers: readonly IPNNetMapPeerNode[];
  readonly lockedOut: boolean;
}

declare global {
  class Go {
    importObject: WebAssembly.Imports;
    run: (instance: WebAssembly.Instance) => Promise<void>;
  }

  function newIPN(config: IPNConfig): IPN;
}
