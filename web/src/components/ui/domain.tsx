import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

const wildcard = "*.";

/**
 * A domain as a name token: the code face on the recessed surface, so a column of domains reads as
 * a column of names rather than a run of text, and a wildcard's `*.` steps back so the part that
 * names the site leads. It is not a chip: a domain is a value, not a label, and a tint would say
 * more than it means.
 */
export function Domain({
  domain,
  className,
}: {
  readonly domain: string;
  readonly className?: string;
}): ReactElement {
  const wild = domain.startsWith(wildcard);
  const name = wild ? domain.slice(wildcard.length) : domain;

  return (
    <span
      title={domain}
      className={cn(
        "inline-flex max-w-full min-w-0 items-center rounded-md bg-kumo-recessed px-1.5 py-0.5 font-mono text-[0.9em] leading-4 text-kumo-default ring ring-kumo-hairline ring-inset",
        className,
      )}
    >
      <span className="truncate">
        {wild ? <span className="text-kumo-subtle">{wildcard}</span> : null}
        {name}
      </span>
    </span>
  );
}

/** Domains one per line; in a table `max` keeps the cell short and counts the rest. */
export function DomainList({
  domains,
  max,
  empty = "None",
  className,
}: {
  readonly domains: readonly string[];
  /** How many domains show before the rest are counted; every one when absent. */
  readonly max?: number;
  readonly empty?: string;
  readonly className?: string;
}): ReactElement {
  if (domains.length === 0) {
    return <span className="text-kumo-subtle">{empty}</span>;
  }

  const shown = max === undefined ? domains : domains.slice(0, max);
  const hidden = domains.slice(shown.length);

  return (
    <ul className={cn("flex min-w-0 flex-col items-start gap-1", className)}>
      {shown.map((domain) => (
        <li key={domain} className="flex max-w-full min-w-0">
          <Domain domain={domain} />
        </li>
      ))}
      {hidden.length === 0 ? null : (
        <li className="text-xs text-kumo-subtle">
          <Tooltip content={hidden.join(", ")}>
            <span>{`+${hidden.length} more`}</span>
          </Tooltip>
        </li>
      )}
    </ul>
  );
}
