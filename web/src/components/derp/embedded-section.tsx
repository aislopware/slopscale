import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { PencilSimpleIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import { EmbeddedDetailsForm } from "~/components/derp/embedded-form.tsx";
import { serverDraft, serverFromDraft, withServer } from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { Code } from "~/components/ui/code.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";
import { SettingsCard, SettingsRow } from "~/components/ui/settings-card.tsx";
import { Note, Status } from "~/components/ui/status.tsx";

function Muted({ children }: { readonly children: string }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
}

/**
 * A fact that runs onto a second line rather than being cut off. The list truncates by default, so
 * a long region name or a warning has no room on a phone.
 */
function regionLabel(server: Derp["effective"]["server"]): string {
  const base = `${server.regionId ?? ""} · ${server.regionCode ?? ""}`;

  if (server.regionName === undefined || server.regionName === "") {
    return base;
  }

  return `${base} · ${server.regionName}`;
}

function stunFact(derp: Derp): Definition {
  if (derp.effective.server.stunEnabled === false) {
    return { label: "STUN", value: <Muted>Off. Machines ask the Tailscale relays instead</Muted> };
  }

  if (derp.relayRunning) {
    return { label: "STUN", value: derp.stunAddr, copy: derp.stunAddr };
  }

  const configured = derp.effective.server.stunAddr ?? "";

  return {
    label: "STUN",
    value:
      configured === "" ? (
        <Muted>Not set</Muted>
      ) : (
        <span className="flex flex-wrap items-baseline justify-end gap-x-2">
          <span>{configured}</span>
          <Muted>not listening</Muted>
        </span>
      ),
  };
}

function regionFact(derp: Derp): Definition {
  const { server } = derp.effective;

  if (!server.enabled) {
    return { label: "Region", value: <Muted>Not published</Muted> };
  }

  if (!derp.autoAddEmbedded) {
    return {
      label: "Region",
      wrap: true,
      value: (
        <span className="flex flex-wrap items-baseline justify-end gap-x-2">
          <span>{regionLabel(server)}</span>
          <Muted>published by the map file, not by these settings</Muted>
        </span>
      ),
    };
  }

  return { label: "Region", value: regionLabel(server), wrap: true };
}

function facts(derp: Derp): readonly Definition[] {
  const { server } = derp.effective;
  const insecure = server.enabled && !derp.serverUrl.startsWith("https://");
  const addresses = [server.ipv4, server.ipv6].filter((ip) => ip !== undefined && ip !== "");

  return [
    {
      label: "Status",
      value: (
        <span className="flex flex-col items-end gap-1">
          <Status tone={derp.relayRunning ? "success" : "neutral"}>
            {derp.relayRunning ? "Running" : "Off"}
          </Status>
          {insecure ? (
            <Note className="text-left">
              Published as insecure because the server URL is not HTTPS
            </Note>
          ) : null}
        </span>
      ),
      // The note is a sentence; cut on one line it lost its start at a phone's width.
      wrap: true,
    },
    { label: "Reached at", value: derp.serverUrl, copy: derp.serverUrl },
    regionFact(derp),
    stunFact(derp),
    {
      label: "Clients admitted",
      value:
        server.verifyClients === true ? (
          "Only this tailnet's machines"
        ) : (
          <Muted>Any Tailscale client that finds it</Muted>
        ),
    },
    {
      label: "Published addresses",
      value:
        addresses.length === 0 ? (
          <Muted>None. Machines resolve the host name</Muted>
        ) : (
          addresses.join(", ")
        ),
    },
  ];
}

/**
 * The relay slopscale runs itself: a switch, and its facts in a card that turns into their fields
 * in place for an edit.
 */
export function EmbeddedSection({
  derp,
  canEdit,
  mutations,
}: {
  readonly derp: Derp;
  readonly canEdit: boolean;
  readonly mutations: DerpMutations;
}): ReactElement {
  const [editing, setEditing] = useState(false);
  const settings = derp.effective;
  const { server } = settings;
  const pending = mutations.set.isPending;
  const canRun = derp.relayAvailable;

  function toggle(on: boolean): void {
    const next = serverFromDraft(serverDraft(server), on);

    mutations.apply(withServer(settings, next), `Embedded relay turned ${on ? "on" : "off"}`);
  }

  return (
    <>
      <SettingsCard>
        <SettingsRow
          title="Run the embedded relay"
          description={
            canRun ? (
              "Serves DERP on the server URL and STUN on its own UDP port. Turning it off moves the machines using it to the next closest relay."
            ) : (
              <>
                The server has no relay key. Set <Code>derp.server.private_key_path</Code> in the
                config file and restart.
              </>
            )
          }
          control={
            <Switch
              aria-label="Run the embedded relay"
              checked={server.enabled}
              disabled={!canEdit || !canRun || pending}
              transitioning={pending}
              onCheckedChange={toggle}
            />
          }
        />
      </SettingsCard>
      {editing ? (
        <EmbeddedDetailsForm
          derp={derp}
          mutations={mutations}
          onDone={() => {
            setEditing(false);
            requestAnimationFrame(() => {
              document.querySelector<HTMLElement>("#embedded-relay-edit")?.focus();
            });
          }}
        />
      ) : (
        <Section
          title="Details"
          description="The relay this server runs on its own URL, so there is always one next to the control server."
          bodyClassName="p-0"
          {...(canEdit && canRun
            ? {
                actions: (
                  <Button
                    id="embedded-relay-edit"
                    variant="secondary"
                    size="sm"
                    icon={PencilSimpleIcon}
                    aria-label="Edit details"
                    onClick={() => {
                      setEditing(true);
                    }}
                  >
                    Edit
                  </Button>
                ),
              }
            : {})}
        >
          <DefinitionList items={facts(derp)} />
        </Section>
      )}
    </>
  );
}
