import { Badge } from "@cloudflare/kumo/components/badge";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import { expiresSoon } from "~/components/keys/status.ts";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
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
    return <span className="text-kumo-inactive">Never</span>;
  }

  if (isPast(date)) {
    return (
      <Badge variant="error" appearance="dot">
        Expired
      </Badge>
    );
  }

  if (expiresSoon(date)) {
    return (
      <Tooltip content={formatAbsolute(date)}>
        <Badge variant="warning" appearance="dot">
          {formatRelative(date)}
        </Badge>
      </Tooltip>
    );
  }

  return (
    <span className="text-kumo-subtle">
      <RelativeTime value={value} />
    </span>
  );
}
