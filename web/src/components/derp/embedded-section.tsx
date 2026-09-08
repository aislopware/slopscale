import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { PencilSimpleIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Derp } from "~/api/queries.ts";
import {
  customRegionIds,
  serverDraft,
  serverError,
  serverFromDraft,
  withServer,
} from "~/components/derp/model.ts";
import type { ServerDraft } from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

function Muted({ children }: { readonly children: string }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
}

function regionLabel(server: Derp["effective"]["server"]): string {
  const base = `${server.regionId ?? ""} · ${server.regionCode ?? ""}`;

  if (server.regionName === undefined || server.regionName === "") {
    return base;
  }

  return `${base} · ${server.regionName}`;
}

function stunFact(derp: Derp): Definition {
  if (derp.relayRunning) {
    return { label: "STUN", value: derp.stunAddr, copy: derp.stunAddr };
  }

  const configured = derp.effective.server.stunAddr ?? "";

  return { label: "STUN", value: configured === "" ? <Muted>Not set</Muted> : configured };
}

function facts(derp: Derp): readonly Definition[] {
  const { server } = derp.effective;
  const insecure = server.enabled && !derp.serverUrl.startsWith("https://");
  const addresses = [server.ipv4, server.ipv6].filter((ip) => ip !== undefined && ip !== "");

  return [
    {
      label: "Status",
      value: (
        <span className="flex items-center gap-2">
          <Badge appearance="dot" variant={derp.relayRunning ? "success" : "neutral"}>
            {derp.relayRunning ? "Running" : "Off"}
          </Badge>
          {insecure ? (
            <Badge variant="warning">Published as insecure, the server URL is not https</Badge>
          ) : null}
        </span>
      ),
    },
    { label: "Reached at", value: derp.serverUrl, copy: derp.serverUrl },
    {
      label: "Region",
      value: server.enabled ? regionLabel(server) : <Muted>Not published</Muted>,
    },
    stunFact(derp),
    {
      label: "Admits",
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
          <Muted>None, machines resolve the host name</Muted>
        ) : (
          addresses.join(", ")
        ),
    },
  ];
}

/** The relay headscale runs itself: a switch, its facts and a form for the details. */
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

    mutations.apply(withServer(settings, next), `Embedded relay ${on ? "on" : "off"}`);
  }

  return (
    <Section
      title="Embedded relay"
      description="The relay this server runs on its own URL, so there is always one next to the control server. Machines pick the closest region by latency."
      bodyClassName="p-0"
      {...(canEdit && canRun
        ? {
            actions: (
              <Button
                variant="secondary"
                size="sm"
                icon={PencilSimpleIcon}
                onClick={() => {
                  setEditing(true);
                }}
              >
                Edit details
              </Button>
            ),
          }
        : {})}
    >
      <SectionRow className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="font-medium text-kumo-strong">Run the embedded relay</span>
          <p className="max-w-prose text-kumo-subtle">
            {canRun
              ? "Serves DERP on the server URL and STUN on its own UDP port, and publishes the region to every machine. Turning it off drops the machines using it onto the next closest relay."
              : "The server has no relay key: set derp.server.private_key_path in the config file and restart."}
          </p>
        </div>
        <span className="flex h-lh shrink-0 items-center">
          <Switch
            aria-label="Run the embedded relay"
            checked={server.enabled}
            disabled={!canEdit || !canRun || pending}
            transitioning={pending}
            onCheckedChange={toggle}
          />
        </span>
      </SectionRow>
      <DefinitionList items={facts(derp)} />
      <DialogRoot open={editing} onOpenChange={setEditing}>
        <DialogContent
          size="base"
          title="Embedded relay details"
          description="How the relay is published to the machines. A change restarts STUN when its address moves; the relay keeps serving."
        >
          <ServerForm
            derp={derp}
            mutations={mutations}
            onDone={() => {
              setEditing(false);
            }}
          />
        </DialogContent>
      </DialogRoot>
    </Section>
  );
}

function ServerForm({
  derp,
  mutations,
  onDone,
}: {
  readonly derp: Derp;
  readonly mutations: DerpMutations;
  readonly onDone: () => void;
}): ReactElement {
  const settings = derp.effective;
  const [draft, setDraft] = useState<ServerDraft>(() => serverDraft(settings.server));
  const [touched, setTouched] = useState(false);
  const issue = serverError(draft, customRegionIds(settings));

  function update(patch: Partial<ServerDraft>): void {
    setDraft((current) => ({ ...current, ...patch }));
  }

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      return;
    }

    mutations.set.mutate(
      { body: withServer(settings, serverFromDraft(draft, settings.server.enabled)) },
      {
        onSuccess: () => {
          toast.success("Embedded relay updated");
          onDone();
        },
      },
    );
  }

  const field = ({
    key,
    label,
    placeholder,
    description,
  }: {
    readonly key: keyof ServerDraft;
    readonly label: string;
    readonly placeholder: string;
    readonly description?: string;
  }): ReactElement => (
    <Input
      className="w-full"
      label={label}
      value={String(draft[key])}
      placeholder={placeholder}
      spellCheck={false}
      autoComplete="off"
      {...(description === undefined ? {} : { description })}
      onChange={(event) => {
        update({ [key]: event.target.value });
      }}
      onBlur={() => {
        setTouched(true);
      }}
    />
  );

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <div className="grid items-start gap-4 sm:grid-cols-2">
        {field({
          key: "regionId",
          label: "Region id",
          placeholder: "999",
          description: "Replaces a fetched region with the same id.",
        })}
        {field({
          key: "regionCode",
          label: "Region code",
          placeholder: "headscale",
          description: "Short code the clients show.",
        })}
      </div>
      {field({
        key: "regionName",
        label: "Region name",
        placeholder: "Headscale Embedded DERP",
        description: "Empty takes the code.",
      })}
      {field({
        key: "stunAddr",
        label: "STUN address",
        placeholder: "0.0.0.0:3478",
        description: "UDP host:port STUN listens on; open it on the firewall.",
      })}
      <div className="grid items-start gap-4 sm:grid-cols-2">
        {field({
          key: "ipv4",
          label: "Public IPv4",
          placeholder: "198.51.100.1",
          description: "Published next to the host name.",
        })}
        {field({
          key: "ipv6",
          label: "Public IPv6",
          placeholder: "2001:db8::1",
          description: "Reached while DNS is down.",
        })}
      </div>
      <Switch
        label="Admit only this tailnet's machines"
        checked={draft.verifyClients}
        onCheckedChange={(on) => {
          update({ verifyClients: on });
        }}
      />
      {touched && issue !== null ? <p className="text-sm text-kumo-danger">{issue}</p> : null}
      <DialogError
        message={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      />
      <FormFooter label="Save" pending={mutations.set.isPending} />
    </form>
  );
}
