import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactNode } from "react";

/**
 * Says why a control is unavailable. A disabled control takes no pointer events, so the tooltip
 * hangs off a span around it, the same way Kumo's own Button carries the title of a disabled
 * button.
 *
 * Pass `reason` only while the control is disabled; without one the child is rendered untouched, so
 * a caller can wrap a control that is sometimes available.
 */
export function DisabledReason({
  reason,
  children,
}: {
  readonly reason: string | undefined;
  readonly children: ReactNode;
}): ReactNode {
  if (reason === undefined || reason === "") {
    return children;
  }

  return (
    <Tooltip content={reason} render={<span className="inline-flex" />}>
      {children}
    </Tooltip>
  );
}
