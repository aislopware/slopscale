import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import { expiresSoon } from "~/components/keys/status.ts";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Status } from "~/components/ui/status.tsx";
import { formatAbsolute, formatRelative, isPast, parseTime } from "~/lib/time.ts";

/** The identifying head of a key: a click-to-copy monospace run. */
export function KeyPrefix({
  text,
  copy,
  label,
}: {
  readonly text: string;
  /** The full secret or prefix put on the clipboard. */
  readonly copy: string;
  readonly label: string;
}): ReactElement {
  return <CopyText value={text} copy={copy} label={label} className="text-kumo-default" />;
}

/** When a key runs out: quiet until the last day, then a warning badge, then an error one. */
export function ExpiryCell({ value }: { readonly value: string | null }): ReactElement {
  const date = parseTime(value);

  if (date === null) {
    return <span className="text-kumo-subtle">Never</span>;
  }

  if (isPast(date)) {
    return <Status tone="danger">Expired</Status>;
  }

  if (expiresSoon(date)) {
    return (
      <Tooltip content={formatAbsolute(date)}>
        <Status tone="warning">{formatRelative(date)}</Status>
      </Tooltip>
    );
  }

  return (
    <span className="text-kumo-subtle">
      <RelativeTime value={value} />
    </span>
  );
}
