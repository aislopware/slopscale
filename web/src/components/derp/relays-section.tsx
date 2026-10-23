import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { cn } from "@cloudflare/kumo/utils";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { Link, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import type { DerpRelay } from "~/api/schema.gen.ts";
import { withoutRegion } from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { rowLinkClass } from "~/components/table/styles.ts";
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
  const navigate = useNavigate();
  const settings = derp.effective;
  const pending = mutations.set.isPending;

  return (
    <Section
      title="Your relays"
      description="Regions of relays you run, merged on top of the fetched maps."
      bodyClassName="p-0"
      actions={
        canEdit ? (
          <LinkButton href="/relays/own/new" variant="secondary" size="sm" icon={PlusIcon}>
            Add region
          </LinkButton>
        ) : (
          <Button variant="secondary" size="sm" icon={PlusIcon} disabled>
            Add region
          </Button>
        )
      }
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
                <Link
                  to="/relays/own/$regionId"
                  params={{ regionId: String(region.id) }}
                  className={cn("font-mono text-sm font-medium", rowLinkClass)}
                >
                  {region.id} · {region.code}
                </Link>
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
            <RowMenu label={`Actions for region ${region.code}`} disabled={pending}>
              <DropdownMenu.Item
                icon={PencilSimpleIcon}
                onClick={() => {
                  void navigate({
                    to: "/relays/own/$regionId",
                    params: { regionId: String(region.id) },
                  });
                }}
              >
                {canEdit ? "Edit" : "View"}
              </DropdownMenu.Item>
              <DropdownMenu.Separator />
              <DropdownMenu.Item
                icon={TrashIcon}
                variant="danger"
                disabled={!canEdit}
                onClick={() => {
                  mutations.apply(withoutRegion(settings, region.id), `Removed ${region.code}`);
                }}
              >
                Remove
              </DropdownMenu.Item>
            </RowMenu>
          </SectionRow>
        ))
      )}
    </Section>
  );
}
