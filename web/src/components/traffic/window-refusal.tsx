import { Button } from "@cloudflare/kumo/components/button";
import type { ReactElement } from "react";

import { ApiError, errorMessage } from "~/api/error.ts";
import { Callout } from "~/components/ui/callout.tsx";

const statusBadRequest = 400;

/**
 * The server's reason for refusing a request. The traffic endpoints' detail names only the
 * operation that failed ("reading traffic"), so the reasons alone read better when there are any.
 */
export function refusalOf(failure: unknown): string {
  const reasons =
    failure instanceof ApiError
      ? (failure.problem?.errors ?? []).flatMap((entry) =>
          entry.message === undefined || entry.message === "" ? [] : [entry.message],
        )
      : [];

  if (reasons.length === 0) {
    return errorMessage(failure);
  }

  const text = reasons.join(". ");

  return `${text.charAt(0).toUpperCase()}${text.slice(1)}.`;
}

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
      title="This range cannot be shown"
      description={refusalOf(failure)}
      action={
        <Button size="sm" variant="secondary" onClick={onReset}>
          Last 24 hours
        </Button>
      }
    />
  );
}
