import { useSyncExternalStore } from "react";

export type Theme = "light" | "dark" | "system";

const storageKey = "headscale-admin.theme";
const themes: readonly Theme[] = ["light", "dark", "system"];
const listeners = new Set<() => void>();
const media = globalThis.matchMedia("(prefers-color-scheme: dark)");

function read(): Theme {
  const stored = globalThis.localStorage.getItem(storageKey);

  return themes.find((candidate) => candidate === stored) ?? "system";
}

function apply(theme: Theme): void {
  const dark = theme === "dark" || (theme === "system" && media.matches);
  document.documentElement.dataset["mode"] = dark ? "dark" : "light";
}

function notify(): void {
  apply(read());

  for (const listener of listeners) {
    listener();
  }
}

media.addEventListener("change", notify);
apply(read());

function subscribe(listener: () => void): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

function setTheme(next: Theme): void {
  globalThis.localStorage.setItem(storageKey, next);
  notify();
}

function serverTheme(): Theme {
  return "system";
}

export const theme = { get: read, set: setTheme, subscribe };

export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, read, serverTheme);
}
