import type { SSHAction, SSHSessionState } from "./types.ts";
import type { IPNStateStorage } from "./wasm-js.d.ts";

export const initialSSHState: SSHSessionState = {
  status: "loading-client",
  ipnState: "NoState",
  progressMessage: null,
  error: null,
  notBuilt: false,
};

export function sshReducer(state: SSHSessionState, action: SSHAction): SSHSessionState {
  switch (action.type) {
    case "START_LOADING": {
      return {
        ...state,
        status: "loading-client",
        error: null,
        notBuilt: false,
      };
    }
    case "CLIENT_LOADED": {
      return {
        ...state,
        status: "joining-tailnet",
        error: null,
      };
    }
    case "NOTIFY_IPN_STATE": {
      const isRunning = action.ipnState === "Running";
      return {
        ...state,
        ipnState: action.ipnState,
        status: state.status === "joining-tailnet" && isRunning ? "connecting" : state.status,
      };
    }
    case "START_CONNECTING": {
      return {
        ...state,
        status: "connecting",
        progressMessage: null,
        error: null,
      };
    }
    case "CONNECTION_PROGRESS": {
      return {
        ...state,
        status: "connecting",
        progressMessage: action.message,
      };
    }
    case "CONNECTED": {
      return {
        ...state,
        status: "connected",
        progressMessage: null,
        error: null,
      };
    }
    case "DONE": {
      return {
        ...state,
        status: "done",
        progressMessage: null,
      };
    }
    case "ERROR": {
      return {
        ...state,
        status: "error",
        error: action.error,
        notBuilt: action.notBuilt ?? false,
      };
    }
    case "RECONNECT": {
      return {
        ...state,
        status: state.ipnState === "Running" ? "connecting" : "joining-tailnet",
        error: null,
        progressMessage: null,
      };
    }
    default: {
      return state;
    }
  }
}

export function createMemoryStorage(): IPNStateStorage {
  const store = new Map<string, string>();
  return {
    getState: (id: string): string => store.get(id) ?? "",
    setState: (id: string, value: string): void => {
      store.set(id, value);
    },
  };
}
