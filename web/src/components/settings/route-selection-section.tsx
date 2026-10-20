import { Select } from "@cloudflare/kumo/components/select";
import type { ReactElement } from "react";

import type { Settings } from "~/api/queries.ts";
import { useSettingsMutation } from "~/components/settings/mutations.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

type RouteSelection = Settings["routeSelection"];

/** Tailscale's names for the modes, in the order its documentation lists them. */
const modes: readonly RouteSelection[] = [
  "active-passive-failover",
  "regional-routing",
  "regional-routing-failover",
  "magicroute",
];

const labels: Record<RouteSelection, string> = {
  "active-passive-failover": "Active-passive failover",
  "regional-routing": "Regional routing",
  "regional-routing-failover": "Regional routing, in-region failover",
  magicroute: "MagicRoute",
};

const descriptions: Record<RouteSelection, string> = {
  "active-passive-failover":
    "Every machine uses the same router for a subnet, connector or service: the first healthy one, until it stops answering. Global exit nodes keep your order.",
  "regional-routing":
    "Each machine uses the healthy router nearest it by measured round trip, else the one in its home DERP region. The nearest global exit nodes come first.",
  "regional-routing-failover":
    "Each machine uses the oldest healthy router in its home DERP region, and leaves the region only when none is left there. Global exit nodes keep your order.",
  magicroute:
    "Tailscale's name for latency-scored selection. Slopscale steers each machine to its nearest healthy router, the same as regional routing.",
};

export function RouteSelectionSection({
  settings,
  canEdit,
}: {
  readonly settings: Settings;
  readonly canEdit: boolean;
}): ReactElement {
  const update = useSettingsMutation();
  const current = settings.routeSelection;

  return (
    <Section
      title="Route selection"
      description="Which machine each client uses when several serve the same subnet, app connector or service."
      bodyClassName="p-0"
    >
      <SettingRow
        title="Mode"
        description={descriptions[current]}
        control={
          <Select
            aria-label="Route selection"
            value={current}
            disabled={!canEdit || update.isPending}
            renderValue={(value) => labels[modes.find((mode) => mode === value) ?? current]}
            onValueChange={(value) => {
              const mode = modes.find((known) => known === value);
              if (mode === undefined || mode === current) {
                return;
              }

              update.mutate(
                { body: { routeSelection: mode } },
                {
                  onSuccess: () => {
                    toast.success(`Route selection set to ${labels[mode]}`);
                  },
                },
              );
            }}
          >
            {modes.map((mode) => (
              <Select.Option key={mode} value={mode}>
                {labels[mode]}
              </Select.Option>
            ))}
          </Select>
        }
      />
    </Section>
  );
}
