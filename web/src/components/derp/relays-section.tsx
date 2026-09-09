import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import type { DerpCustomRegion, DerpRelay } from "~/api/schema.gen.ts";
import { withoutRegion } from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { RegionForm } from "~/components/derp/region-form.tsx";
import { DialogContent, DialogRoot } from "~/components/ui/dialog.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";

function relaySummary(relay: DerpRelay): string {
  const parts = [relay.hostName];

  if (relay.ipv4 !== undefined && relay.ipv4 !== "") {
    parts.push(relay.ipv4);
  }

  if (relay.ipv6 !== undefined && relay.ipv6 !== "") {
    parts.push(relay.ipv6);
  }

  if (relay.derpPort !== undefined && relay.derpPort !== 0) {
    parts.push(`port ${relay.derpPort}`);
  }

  if (relay.stunPort !== undefined && relay.stunPort !== 0) {
    parts.push(`stun ${relay.stunPort}`);
  }

  if (relay.stunOnly === true) {
    parts.push("STUN only");
  }

  return parts.join(" · ");
}

/** Regions of relays the operator runs, added on top of the fetched maps. */
export function RelaysSection({
  derp,
  canEdit,
  mutations,
}: {
  readonly derp: Derp;
  readonly canEdit: boolean;
  readonly mutations: DerpMutations;
}): ReactElement {
  const [dialog, setDialog] = useState<"closed" | "new" | DerpCustomRegion>("closed");
  const settings = derp.effective;
  const pending = mutations.set.isPending;

  return (
    <Section
      title="Your relays"
      description="Regions of relays you run, merged on top of the fetched maps."
      bodyClassName="p-0"
      {...(canEdit
        ? {
            actions: (
              <Button
                variant="secondary"
                icon={PlusIcon}
                onClick={() => {
                  setDialog("new");
                }}
              >
                Add region
              </Button>
            ),
          }
        : {})}
    >
      {settings.regions.length === 0 ? (
        <SectionEmpty
          title="No relays of your own"
          description="Add a region for a relay you run yourself."
        />
      ) : (
        settings.regions.map((region) => (
          <SectionRow key={region.id} className="flex items-start justify-between gap-4 py-2.5">
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="flex items-baseline gap-2">
                <span className="font-mono text-sm font-medium text-kumo-strong">
                  {region.id} · {region.code}
                </span>
                {region.name === undefined ||
                region.name === "" ||
                region.name === region.code ? null : (
                  <span className="text-sm text-kumo-subtle">{region.name}</span>
                )}
              </span>
              <ul className="flex flex-col gap-0.5">
                {(region.nodes ?? []).map((relay) => (
                  <li
                    key={relay.name ?? relay.hostName}
                    className="min-w-0 font-mono text-sm break-all text-kumo-subtle"
                  >
                    {relaySummary(relay)}
                  </li>
                ))}
              </ul>
            </div>
            {canEdit ? (
              <RowMenu label={`Actions for region ${region.code}`} disabled={pending}>
                <DropdownMenu.Item
                  icon={PencilSimpleIcon}
                  onClick={() => {
                    setDialog(region);
                  }}
                >
                  Edit…
                </DropdownMenu.Item>
                <DropdownMenu.Separator />
                <DropdownMenu.Item
                  icon={TrashIcon}
                  variant="danger"
                  onClick={() => {
                    mutations.apply(withoutRegion(settings, region.id), `Removed ${region.code}`);
                  }}
                >
                  Remove
                </DropdownMenu.Item>
              </RowMenu>
            ) : null}
          </SectionRow>
        ))
      )}
      <DialogRoot
        open={dialog !== "closed"}
        onOpenChange={(open) => {
          if (!open) {
            setDialog("closed");
          }
        }}
      >
        <DialogContent
          size="lg"
          title={dialog === "new" ? "Add region" : "Edit region"}
          description="A region groups relays that are close to each other. A machine picks the region with the lowest latency."
        >
          <RegionForm
            editing={dialog === "new" || dialog === "closed" ? null : dialog}
            derp={derp}
            mutations={mutations}
            onDone={() => {
              setDialog("closed");
            }}
          />
        </DialogContent>
      </DialogRoot>
    </Section>
  );
}
