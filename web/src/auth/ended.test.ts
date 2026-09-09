import { describe, expect, it, vi } from "vitest";

import { fetchClient } from "~/api/client.ts";
import { onSessionEnd } from "~/auth/ended.ts";

/** A server whose cookie has stopped working: every request is refused. */
const refused = (): Promise<Response> =>
  Promise.resolve(new Response("", { status: 401, statusText: "Unauthorized" }));

/** The listeners run on a microtask, so a test has to let one pass. */
const settle = (): Promise<void> =>
  new Promise((resolve) => {
    queueMicrotask(() => {
      resolve();
    });
  });

describe(onSessionEnd, () => {
  it("hears a refused request once, however many were refused together", async () => {
    const ended = vi.fn<() => void>();
    const stop = onSessionEnd(ended);

    await Promise.allSettled([
      fetchClient.GET("/api/v1/node", { fetch: refused }),
      fetchClient.GET("/api/v1/user", { fetch: refused }),
    ]);
    await settle();
    stop();

    expect(ended).toHaveBeenCalledOnce();
  });

  it("does not hear the guards asking who is signed in", async () => {
    const ended = vi.fn<() => void>();
    const stop = onSessionEnd(ended);

    await Promise.allSettled([fetchClient.GET("/api/v1/whoami", { fetch: refused })]);
    await settle();
    stop();

    expect(ended).not.toHaveBeenCalled();
  });
});
