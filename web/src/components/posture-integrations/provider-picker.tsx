import { cn } from "@cloudflare/kumo/utils";
import { ShieldCheckIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { PostureProvider } from "~/api/queries.ts";
import { Section } from "~/components/ui/section.tsx";

const tileIconSize = 20;
const tileClass =
  "flex min-h-[58px] min-w-0 items-center gap-3 rounded-lg bg-kumo-tint px-4 py-2 text-left";

/**
 * The providers as the dashboard's posture type picker draws them: a two-column grid of tinted
 * tiles, each with its icon, its name and the prefix its attributes sync under. Each tile opens the
 * new integration page with that provider chosen. A role that may not add one sees them dimmed.
 */
export function ProviderTiles({
  providers,
  disabled,
}: {
  readonly providers: readonly PostureProvider[];
  /** The operator's role may not add an integration; the tiles stay for reading. */
  readonly disabled: boolean;
}): ReactElement {
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {providers.map((provider) => {
        const body = (
          <>
            <ShieldCheckIcon
              size={tileIconSize}
              aria-hidden
              className="shrink-0 text-kumo-subtle"
            />
            <span className="flex min-w-0 flex-col">
              <span className="truncate font-medium text-kumo-default">{provider.label}</span>
              <span className="truncate font-mono text-xs text-kumo-subtle">
                {provider.prefix}…
              </span>
            </span>
          </>
        );

        return disabled ? (
          <div key={provider.provider} aria-disabled className={cn(tileClass, "opacity-50")}>
            {body}
          </div>
        ) : (
          <Link
            key={provider.provider}
            to="/integrations/posture/new"
            search={{ provider: provider.provider }}
            className={cn(
              tileClass,
              "hover:bg-kumo-fill focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
            )}
          >
            {body}
          </Link>
        );
      })}
    </div>
  );
}

/** The tiles in place of the empty list, so a tailnet with no integration starts from them. */
export function ProviderPicker({
  providers,
  disabled,
}: {
  readonly providers: readonly PostureProvider[];
  readonly disabled: boolean;
}): ReactElement {
  return (
    <Section
      title="Select a provider"
      description="Connect an endpoint security or device management service. Its attributes sync to machines by serial number, ready for postures to check."
      bodyClassName="p-4"
    >
      <ProviderTiles providers={providers} disabled={disabled} />
    </Section>
  );
}
