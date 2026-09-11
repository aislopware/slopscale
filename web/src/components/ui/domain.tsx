import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { seededColours } from "~/lib/hue.ts";
import { tagHueSteps } from "~/lib/tag.ts";

const wildcard = "*.";

/** Second-level labels under a two-letter country code that are registries in their own right. */
const registrySecondLevels = new Set(["co", "com", "net", "org", "gov", "edu", "ac"]);
const countryCodeLength = 2;
const registrableLabels = 2;

/**
 * The part of a domain someone registered, `jmango360.dev` of `kibana-prod.jmango360.dev`, and what
 * comes before it: the last two labels, or three under `co.uk`-style registries.
 */
export function splitDomain(name: string): { readonly sub: string; readonly site: string } {
  const labels = name.split(".");
  const [tld = "", second = ""] = labels.toReversed();
  let keep = registrableLabels;

  if (tld.length === countryCodeLength && registrySecondLevels.has(second)) {
    keep += 1;
  }

  if (labels.length <= keep) {
    return { sub: "", site: name };
  }

  const cut = labels.length - keep;

  return { sub: `${labels.slice(0, cut).join(".")}.`, site: labels.slice(cut).join(".") };
}

/**
 * A domain as a name token: the code face on the recessed surface, so a column of domains reads as
 * a column of names rather than a run of text. The registered site leads and its subdomain and a
 * wildcard's `*.` step back, and a dot in a hue seeded by the site keys the family, so a list of
 * hosts under two sites reads as two groups before any name is read. It is not a chip: a domain is
 * a value, not a label, and a tint on the whole would say more than it means.
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
  const { sub, site } = splitDomain(name);

  return (
    <span
      title={domain}
      className={cn(
        "inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-md bg-kumo-recessed py-0.5 pr-2 pl-1.5 font-mono text-[0.9em] leading-4 text-kumo-default ring ring-kumo-hairline ring-inset",
        className,
      )}
    >
      <span
        aria-hidden
        className="size-1.5 shrink-0 rounded-full"
        style={{ backgroundColor: seededColours(site, tagHueSteps).color }}
      />
      <span className="truncate">
        {wild ? <span className="text-kumo-subtle">{wildcard}</span> : null}
        {sub === "" ? null : <span className="text-kumo-subtle">{sub}</span>}
        <span className="font-medium">{site}</span>
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
