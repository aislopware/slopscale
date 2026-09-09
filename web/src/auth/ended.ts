/**
 * The one signal that the session is over, whether the operator signed out or the server stopped
 * accepting the cookie. The router listens and drops everything the session could see, so the
 * guards ask the server again and land on the sign-in page with the way back.
 */

type Listener = () => void;

const listeners = new Set<Listener>();

let pending = false;

/** Runs listener after the session ends; returns the way to stop listening. */
export function onSessionEnd(listener: Listener): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

/**
 * Tells the listeners once, however many requests found out at the same time: a page's queries fail
 * together when the cookie stops working.
 */
export function sessionEnded(): void {
  if (pending) {
    return;
  }

  pending = true;
  queueMicrotask(() => {
    pending = false;

    for (const listener of listeners) {
      listener();
    }
  });
}
