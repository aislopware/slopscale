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
 * A domain as a name token in a tint seeded by the registered site, so a list of hosts under two
 * sites reads as two groups before any name is read. The tint carries the grouping itself: a
 * coloured dot beside grey text made the token look like a status light, and at 6px the hue it
 * carried was the one thing on the row too small to compare. The registered site leads, its
 * subdomain and a wildcard's `*.` step back within the tint, and the code face keeps the token a
 * value rather than a `Tag`'s label.
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
        "inline-flex max-w-full min-w-0 items-center rounded-md px-1.5 py-0.5 font-mono text-[0.9em] leading-4 ring ring-kumo-line ring-inset",
        className,
      )}
      style={seededColours(site, tagHueSteps)}
    >
      <span className="truncate">
        {wild ? <span className="opacity-60">{wildcard}</span> : null}
        {sub === "" ? null : <span className="opacity-60">{sub}</span>}
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
