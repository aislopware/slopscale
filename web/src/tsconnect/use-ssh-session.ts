import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";

import { fetchClient } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import type { components } from "~/api/schema.gen.ts";

import { loadTsConnect, TsConnectNotBuiltError } from "./load.ts";
import { createMemoryStorage, initialSSHState, sshReducer } from "./state.ts";
import type { SSHAction, SSHSessionState } from "./types.ts";
import type { IPN, IPNConfig, IPNSSHSession, IPNState } from "./wasm-js.d.ts";

export type SSHSession = components["schemas"]["SSHSession"];

export interface UseSSHSessionResult {
  readonly state: SSHSessionState;
  readonly session: SSHSession | null;
  /** The draft in the username field. Editing it leaves the running session alone. */
  readonly username: string;
  readonly setUsername: Dispatch<SetStateAction<string>>;
  /** The account the terminal dials as: the draft as it stood at the last connect. */
  readonly connectionUsername: string;
  readonly ipn: IPN | null;
  /** Bumped to give the terminal a fresh SSH session over the same tailnet client. */
  readonly sessionKey: number;
  readonly reconnect: () => void;
  readonly disconnect: () => void;
  readonly onConnectionProgress: (message: string) => void;
  readonly onConnected: () => void;
  readonly onDone: () => void;
  readonly registerSession: (session: IPNSSHSession | null) => void;
}

type InitResult =
  | { readonly ok: true; readonly session: SSHSession; readonly newIPN: (config: IPNConfig) => IPN }
  | { readonly ok: false; readonly error: string; readonly notBuilt: boolean };

/** The account to offer when the server has no better guess for the target machine. */
const fallbackUsername = "root";

async function fetchAndLoad(nodeId: string, baseUrl: string): Promise<InitResult> {
  try {
    const [apiResponse, newIPNFunc] = await Promise.all([
      fetchClient.POST("/api/v1/ssh-session", { body: { nodeId } }),
      loadTsConnect(baseUrl),
    ]);

    const { data } = apiResponse;
    if (data === undefined) {
      return { ok: false, error: "The server returned no session.", notBuilt: false };
    }
    if (!data.target.online) {
      return { ok: false, error: "The machine is offline.", notBuilt: false };
    }
    if (!data.target.sshServer) {
      return { ok: false, error: "The machine does not run Tailscale SSH.", notBuilt: false };
    }

    return { ok: true, session: data, newIPN: newIPNFunc };
  } catch (error: unknown) {
    if (error instanceof TsConnectNotBuiltError) {
      return { ok: false, error: error.message, notBuilt: true };
    }

    return { ok: false, error: errorMessage(error), notBuilt: false };
  }
}

/**
 * Joins the tailnet with the one-time auth key from the API. The node is ephemeral, so its state
 * lives in memory and disappears with the tab.
 */
function startIpn(
  session: SSHSession,
  newIPN: (config: IPNConfig) => IPN,
  dispatch: Dispatch<SSHAction>,
): IPN {
  const ipn = newIPN({
    controlURL: session.controlUrl,
    authKey: session.authKey,
    hostname: session.hostname,
    stateStorage: createMemoryStorage(),
  });

  ipn.run({
    notifyState: (ipnState: IPNState): void => {
      dispatch({ type: "NOTIFY_IPN_STATE", ipnState });
    },
    notifyNetMap: (): void => {
      // The SSH target comes from the API, so the console never reads the netmap.
    },
    notifyBrowseToURL: (): void => {
      // An auth key joins the tailnet outright; there is no interactive login to send anyone to.
    },
    notifyPanicRecover: (panicError: string): void => {
      dispatch({ type: "ERROR", error: panicError, notBuilt: false });
    },
  });

  return ipn;
}

/**
 * Drives one in-browser SSH session: it asks the API for a key, loads the wasm client, joins the
 * tailnet and hands the running client to the terminal. Unmounting logs the ephemeral node out.
 */
export function useSSHSession(nodeId: string, baseUrl?: string): UseSSHSessionResult {
  const resolvedBase = baseUrl ?? import.meta.env.BASE_URL;
  const [state, dispatch] = useReducer(sshReducer, initialSSHState);
  const [session, setSession] = useState<SSHSession | null>(null);
  const [username, setUsername] = useState<string>("");
  const [connectionUsername, setConnectionUsername] = useState<string>("");
  const [ipn, setIpn] = useState<IPN | null>(null);
  const [sessionKey, setSessionKey] = useState<number>(0);

  const ipnRef = useRef<IPN | null>(null);
  const activeSshSessionRef = useRef<IPNSSHSession | null>(null);
  const restartRef = useRef<(() => void) | null>(null);
  // The draft as it stands, readable from the async start: the session is fetched while the page is
  // already on screen, so by the time the server's guess arrives the field may not be empty.
  const draftRef = useRef("");

  useEffect(() => {
    draftRef.current = username;
  }, [username]);

  const closeActiveSession = useCallback((): void => {
    if (activeSshSessionRef.current === null) {
      return;
    }
    activeSshSessionRef.current.close();
    activeSshSessionRef.current = null;
  }, []);

  useEffect(() => {
    let cancelled = false;
    // The server's guess only fills the empty field: after that the draft is the operator's, and a
    // restart must dial as whoever they committed rather than silently going back to the guess.
    let usernameOffered = false;

    const teardown = (): void => {
      if (activeSshSessionRef.current !== null) {
        activeSshSessionRef.current.close();
        activeSshSessionRef.current = null;
      }
      if (ipnRef.current !== null) {
        ipnRef.current.logout();
        ipnRef.current = null;
      }
    };

    const start = async (): Promise<void> => {
      const result = await fetchAndLoad(nodeId, resolvedBase);
      if (cancelled) {
        return;
      }
      if (!result.ok) {
        dispatch({ type: "ERROR", error: result.error, notBuilt: result.notBuilt });
        return;
      }

      setSession(result.session);
      if (!usernameOffered) {
        usernameOffered = true;

        // Whatever is already in the field wins: the machine's own suggestion may have arrived
        // first, or the operator may have typed while the client was still loading.
        const offered =
          draftRef.current === ""
            ? result.session.target.username || fallbackUsername
            : draftRef.current;

        setUsername(offered);
        setConnectionUsername(offered);
      }

      const instance = startIpn(result.session, result.newIPN, dispatch);
      ipnRef.current = instance;
      setIpn(instance);
      dispatch({ type: "CLIENT_LOADED" });
    };

    restartRef.current = (): void => {
      teardown();
      setIpn(null);
      setSession(null);
      dispatch({ type: "START_LOADING" });
      void start();
    };

    void start();

    return (): void => {
      cancelled = true;
      restartRef.current = null;
      teardown();
    };
  }, [nodeId, resolvedBase]);

  const disconnect = useCallback((): void => {
    closeActiveSession();
    dispatch({ type: "DONE" });
  }, [closeActiveSession]);

  const reconnect = useCallback((): void => {
    closeActiveSession();

    // Reconnect is the only thing that commits the field, so typing never dials.
    setConnectionUsername(username);

    // A client that is already on the tailnet only needs a new SSH session; anything else needs a
    // fresh auth key, because the one the API handed out is single use.
    if (ipnRef.current !== null && state.ipnState === "Running") {
      dispatch({ type: "RECONNECT" });
      setSessionKey((previous) => previous + 1);
      return;
    }

    restartRef.current?.();
  }, [closeActiveSession, state.ipnState, username]);

  const onConnectionProgress = useCallback((message: string): void => {
    dispatch({ type: "CONNECTION_PROGRESS", message });
  }, []);

  const onConnected = useCallback((): void => {
    dispatch({ type: "CONNECTED" });
  }, []);

  const onDone = useCallback((): void => {
    activeSshSessionRef.current = null;
    dispatch({ type: "DONE" });
  }, []);

  const registerSession = useCallback((sshSession: IPNSSHSession | null): void => {
    activeSshSessionRef.current = sshSession;
  }, []);

  return {
    state,
    session,
    username,
    setUsername,
    connectionUsername,
    ipn,
    sessionKey,
    reconnect,
    disconnect,
    onConnectionProgress,
    onConnected,
    onDone,
    registerSession,
  };
}
