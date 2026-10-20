import { ShieldCheckIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { PostureProvider } from "~/api/queries.ts";
import { Section } from "~/components/ui/section.tsx";

const tileIconSize = 20;

/**
 * The providers as the dashboard's posture type picker draws them: a two-column grid of tinted
 * tiles, each with its icon, its name and the prefix its attributes sync under. Picking one opens
 * the form with that provider chosen. It stands in for the empty table, so a tailnet with no
 * integration starts from what it can connect.
 */
export function ProviderPicker({
  providers,
  disabled,
  onPick,
}: {
  readonly providers: readonly PostureProvider[];
  /** The operator's role may not add an integration; the tiles stay for reading. */
  readonly disabled: boolean;
  readonly onPick: (provider: string) => void;
}): ReactElement {
  return (
    <Section
      title="Select a provider"
      description="Connect an endpoint security or device management service. Its attributes sync to machines by serial number, ready for postures to check."
      bodyClassName="grid gap-2 p-4 sm:grid-cols-2"
    >
      {providers.map((provider) => (
        <button
          key={provider.provider}
          type="button"
          disabled={disabled}
          className="flex min-h-[58px] min-w-0 items-center gap-3 rounded-lg bg-kumo-tint px-4 py-2 text-left hover:bg-kumo-fill focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
          onClick={() => {
            onPick(provider.provider);
          }}
        >
          <ShieldCheckIcon size={tileIconSize} aria-hidden className="shrink-0 text-kumo-subtle" />
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-kumo-default">{provider.label}</span>
            <span className="truncate font-mono text-xs text-kumo-subtle">{provider.prefix}…</span>
          </span>
        </button>
      ))}
    </Section>
  );
}
