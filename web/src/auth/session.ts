import { useSyncExternalStore } from "react";

const storageKey = "headscale-admin.api-key";

type Listener = () => void;

const listeners = new Set<Listener>();

function read(): string | null {
  try {
    return globalThis.localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

function notify(): void {
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: Listener): () => void {
  listeners.add(listener);

  const onStorage = (event: StorageEvent): void => {
    if (event.key === storageKey || event.key === null) {
      listener();
    }
  };

  globalThis.addEventListener("storage", onStorage);

  return () => {
    listeners.delete(listener);
    globalThis.removeEventListener("storage", onStorage);
  };
}

function setKey(apiKey: string): void {
  globalThis.localStorage.setItem(storageKey, apiKey);
  notify();
}

function clearKey(): void {
  globalThis.localStorage.removeItem(storageKey);
  notify();
}

function serverKey(): null {
  return null;
}

/**
 * The API key the console authenticates with, kept in localStorage so a reload keeps the operator
 * signed in. Every reader subscribes through `useApiKey`, so signing out from one place updates the
 * whole tree.
 */
export const session = { get: read, set: setKey, clear: clearKey, subscribe };

export function useApiKey(): string | null {
  return useSyncExternalStore(subscribe, read, serverKey);
}
