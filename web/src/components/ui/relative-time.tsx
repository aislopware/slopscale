import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import { formatAbsolute, formatRelative, parseTime } from "~/lib/time.ts";

export function RelativeTime({
  value,
  never = "Never",
}: {
  readonly value: string | null | undefined;
  readonly never?: string;
}): ReactElement {
  const date = parseTime(value);

  if (date === null) {
    return <span className="text-kumo-subtle">{never}</span>;
  }

  return (
    <Tooltip content={formatAbsolute(date)}>
      <time dateTime={date.toISOString()}>{formatRelative(date)}</time>
    </Tooltip>
  );
}
