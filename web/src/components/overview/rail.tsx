import { Switch } from "@cloudflare/kumo/components/switch";
import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { serverInfoQuery, settingsQuery } from "~/api/queries.ts";
import type { Settings } from "~/api/queries.ts";
import type { UpdateSettingsRequestBody } from "~/api/schema.gen.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useSettingsMutation } from "~/components/settings/mutations.ts";
import { KeyValueList } from "~/components/ui/definition-list.tsx";
import { RailList, RailRow, RailSection } from "~/components/ui/page.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { docsBase, docsUrl } from "~/lib/docs.ts";

/** A rail link in the dashboard's 12px link style; the router's Link, so it keeps the base path. */
const railLinkClass =
  "inline-flex w-fit items-center gap-1 text-xs text-kumo-link hover:text-kumo-link/70";

/** A 13px link on its own row of a rail card, as the zone overview's "Get your API token". */
const cardLinkClass =
  "inline-flex w-fit items-center gap-1 text-sm text-kumo-link hover:text-kumo-link/70";

const externalIconClass = "size-3";

interface ApprovalSwitch {
  readonly title: string;
  readonly description: string;
  readonly read: (settings: Settings) => boolean;
  readonly write: (on: boolean) => UpdateSettingsRequestBody;
}

/** The two switches that decide what waits on this page, as the zone overview's quick actions. */
const approvalSwitches: readonly ApprovalSwitch[] = [
  {
    title: "Device approval",
    description: "New machines wait for an administrator. Off approves every machine waiting.",
    read: (settings) => settings.devicesApprovalOn,
    write: (on) => ({ devicesApprovalOn: on }),
  },
  {
    title: "User approval",
    description:
      "New users wait before their machines are accepted. Off approves every user waiting.",
    read: (settings) => settings.usersApprovalOn,
    write: (on) => ({ usersApprovalOn: on }),
  },
];

function ApprovalRows({
  settings,
  canEdit,
}: {
  readonly settings: Settings;
  readonly canEdit: boolean;
}): ReactElement {
  const update = useSettingsMutation();

  return (
    <RailList>
      {approvalSwitches.map((row) => (
        <RailRow
          key={row.title}
          title={row.title}
          description={
            <span className="flex flex-col gap-0.5">
              <span>{row.description}</span>
              <Link to="/settings/tailnet" hash="approval" className={railLinkClass}>
                Approval settings
              </Link>
            </span>
          }
          control={
            <Switch
              aria-label={row.title}
              checked={row.read(settings)}
              disabled={!canEdit || update.isPending}
              transitioning={update.isPending}
              onCheckedChange={(on) => {
                update.mutate(
                  { body: row.write(on) },
                  {
                    onSuccess: () => {
                      toast.success(`${row.title} ${on ? "on" : "off"}`);
                    },
                  },
                );
              }}
            />
          }
        />
      ))}
    </RailList>
  );
}

/**
 * The zone overview's API card: the identifiers a script needs, each with a copy button, and the
 * ways to a credential and the reference.
 */
function ApiCard(): ReactElement | null {
  const info = useQuery(serverInfoQuery);

  if (info.data === undefined) {
    return null;
  }

  return (
    <Section title="API" bodyClassName="p-0">
      <KeyValueList
        items={[
          {
            key: "tailnet",
            label: "Tailnet ID",
            value: <span className="font-mono text-[0.9em]">{info.data.tailnetId}</span>,
            copy: info.data.tailnetId,
            copyLabel: "Copy tailnet ID",
          },
          {
            key: "server",
            label: "Server URL",
            value: info.data.serverUrl,
            copy: info.data.serverUrl,
            copyLabel: "Copy server URL",
          },
          {
            key: "links",
            label: "Get started",
            value: (
              <span className="flex flex-col gap-2 py-0.5">
                <Link to="/keys/api" className={cardLinkClass}>
                  Get your API key
                </Link>
                <a
                  href={docsUrl("ref/api")}
                  target="_blank"
                  rel="noreferrer"
                  className={cardLinkClass}
                >
                  API documentation
                  <ArrowSquareOutIcon aria-hidden className={externalIconClass} />
                </a>
              </span>
            ),
          },
        ]}
      />
    </Section>
  );
}

/**
 * The overview's right rail, as the Cloudflare zone overview and Workers list have one: the
 * tailnet's numbers first, then the switches that decide what waits for approval, the API card, and
 * where to read more. A part the caller may not read is left out rather than shown empty.
 */
export function OverviewRail({
  me,
  tiles,
}: {
  readonly me: Me;
  /** The tailnet's numbers; null when the caller may read none of them. */
  readonly tiles: ReactElement | null;
}): ReactElement {
  const readsSettings = can(me, "feature_settings:read");
  const settings = useQuery({ ...settingsQuery, enabled: readsSettings });

  return (
    <>
      {tiles === null ? null : (
        <RailSection title="Tailnet" size="lg">
          {tiles}
        </RailSection>
      )}
      {settings.data === undefined ? null : (
        <RailSection title="Approval">
          <ApprovalRows settings={settings.data} canEdit={can(me, "feature_settings")} />
        </RailSection>
      )}
      {readsSettings ? <ApiCard /> : null}
      <RailSection title="Support resources">
        <RailList>
          <RailRow
            title="Documentation"
            description="Setting up, connecting machines and every page of this console."
            link={{ href: docsBase, label: "Read the docs", external: true }}
          />
          <RailRow
            title="Connect a machine"
            description="Install the client and point it at this server."
            link={{
              href: docsUrl("usage/getting-started"),
              label: "Getting started",
              external: true,
            }}
          />
        </RailList>
      </RailSection>
    </>
  );
}
