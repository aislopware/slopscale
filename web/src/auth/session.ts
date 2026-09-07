import { fetchClient } from "~/api/client.ts";

/**
 * The console signs in only through the identity provider. The browser holds an HttpOnly cookie the
 * console cannot read, so there is no client-side sign-in state: whether the operator is signed in
 * is the server's answer to `/api/v1/whoami`, asked by the route guards.
 */

type Listener = () => void;

const listeners = new Set<Listener>();

/** Runs listener after a sign-out, so the router can drop its caches and re-run the guards. */
export function onSignOut(listener: Listener): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

/**
 * Ends the session on the server, which clears its cookie, then lets the router re-run the guards
 * and land on the sign-in page. A server that cannot be reached is treated the same: the guards
 * will find out on the next request.
 */
export async function signOut(): Promise<void> {
  try {
    await fetchClient.DELETE("/api/v1/auth/session");
  } catch {
    // Nothing to keep here; the session is either gone or unreachable.
  }

  for (const listener of listeners) {
    listener();
  }
}

/** The console's own path for a router location, as the server's redirect parameter wants it. */
export function consolePath(location: string): string {
  return location.startsWith("/admin/") ? location : `/admin${location}`;
}
