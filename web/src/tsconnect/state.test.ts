import { describe, expect, it } from "vitest";

import { createMemoryStorage, initialSSHState, sshReducer } from "~/tsconnect/state.ts";
import type { SSHSessionState } from "~/tsconnect/types.ts";

describe(sshReducer, () => {
  it("starts in loading-client state", () => {
    expect(initialSSHState.status).toBe("loading-client");
    expect(initialSSHState.ipnState).toBe("NoState");
    expect(initialSSHState.error).toBeNull();
    expect(initialSSHState.notBuilt).toBe(false);
  });

  it("transitions to joining-tailnet on CLIENT_LOADED", () => {
    const next = sshReducer(initialSSHState, { type: "CLIENT_LOADED" });
    expect(next.status).toBe("joining-tailnet");
    expect(next.error).toBeNull();
  });

  it("updates ipnState on NOTIFY_IPN_STATE and transitions to connecting when Running", () => {
    const stateInJoining: SSHSessionState = {
      ...initialSSHState,
      status: "joining-tailnet",
    };

    const starting = sshReducer(stateInJoining, {
      type: "NOTIFY_IPN_STATE",
      ipnState: "Starting",
    });
    expect(starting.ipnState).toBe("Starting");
    expect(starting.status).toBe("joining-tailnet");

    const running = sshReducer(starting, {
      type: "NOTIFY_IPN_STATE",
      ipnState: "Running",
    });
    expect(running.ipnState).toBe("Running");
    expect(running.status).toBe("connecting");
  });

  it("handles CONNECTION_PROGRESS during connecting", () => {
    const connectingState: SSHSessionState = {
      ...initialSSHState,
      status: "connecting",
    };

    const next = sshReducer(connectingState, {
      type: "CONNECTION_PROGRESS",
      message: "SSH connection established…",
    });
    expect(next.status).toBe("connecting");
    expect(next.progressMessage).toBe("SSH connection established…");
  });

  it("transitions to connected on CONNECTED", () => {
    const connectingState: SSHSessionState = {
      ...initialSSHState,
      status: "connecting",
      progressMessage: "Connecting…",
    };

    const next = sshReducer(connectingState, { type: "CONNECTED" });
    expect(next.status).toBe("connected");
    expect(next.progressMessage).toBeNull();
    expect(next.error).toBeNull();
  });

  it("transitions to done on DONE", () => {
    const connectedState: SSHSessionState = {
      ...initialSSHState,
      status: "connected",
    };

    const next = sshReducer(connectedState, { type: "DONE" });
    expect(next.status).toBe("done");
    expect(next.progressMessage).toBeNull();
  });

  it("transitions to error and captures notBuilt flag when client was not built", () => {
    const err = sshReducer(initialSSHState, {
      type: "ERROR",
      error: "Tailscale Connect client was not built (make wasm)",
      notBuilt: true,
    });
    expect(err.status).toBe("error");
    expect(err.error).toBe("Tailscale Connect client was not built (make wasm)");
    expect(err.notBuilt).toBe(true);
  });

  it("transitions to error for general errors", () => {
    const otherErr = sshReducer(initialSSHState, {
      type: "ERROR",
      error: "Machine is offline",
    });
    expect(otherErr.status).toBe("error");
    expect(otherErr.error).toBe("Machine is offline");
    expect(otherErr.notBuilt).toBe(false);
  });

  it("handles RECONNECT when ipn is Running vs not Running", () => {
    const doneRunningState: SSHSessionState = {
      ...initialSSHState,
      status: "done",
      ipnState: "Running",
    };
    const reconnectRunning = sshReducer(doneRunningState, { type: "RECONNECT" });
    expect(reconnectRunning.status).toBe("connecting");

    const doneStoppedState: SSHSessionState = {
      ...initialSSHState,
      status: "done",
      ipnState: "Stopped",
    };
    const reconnectStopped = sshReducer(doneStoppedState, { type: "RECONNECT" });
    expect(reconnectStopped.status).toBe("joining-tailnet");
  });
});

describe(createMemoryStorage, () => {
  it("stores and retrieves key-value state in memory", () => {
    const storage = createMemoryStorage();
    expect(storage.getState("test-key")).toBe("");

    storage.setState("test-key", "test-val");
    expect(storage.getState("test-key")).toBe("test-val");

    storage.setState("test-key", "updated-val");
    expect(storage.getState("test-key")).toBe("updated-val");
  });
});
