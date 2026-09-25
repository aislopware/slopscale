import { Button } from "@cloudflare/kumo/components/button";
import type { ReactElement } from "react";

import { ApiError, errorMessage } from "~/api/error.ts";
import { Callout } from "~/components/ui/callout.tsx";

const statusBadRequest = 400;

/** Whether the server refused the window itself, such as a custom range longer than it reads. */
export function isRefusedWindow(failure: unknown): boolean {
  return failure instanceof ApiError && failure.status === statusBadRequest;
}

/**
 * Waits for a page's reads. A refused window stays on the page, next to the controls that change
 * it; any other failure goes to the route's error page as usual.
 */
export async function loadWindow(loads: readonly Promise<unknown>[]): Promise<void> {
  try {
    await Promise.all(loads);
  } catch (error) {
    if (!isRefusedWindow(error)) {
      throw error;
    }
  }
}

/** The server's reason for refusing the window, with a way back to the default one. */
export function WindowRefusal({
  failure,
  onReset,
}: {
  readonly failure: unknown;
  readonly onReset: () => void;
}): ReactElement | null {
  if (!isRefusedWindow(failure)) {
    return null;
  }

  return (
    <Callout
      tone="error"
      title="The server would not read this window"
      description={errorMessage(failure)}
      action={
        <Button size="sm" variant="secondary" onClick={onReset}>
          Last 24 hours
        </Button>
      }
    />
  );
}
