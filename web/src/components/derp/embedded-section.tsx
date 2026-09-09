import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { PencilSimpleIcon } from "@phosphor-icons/react";
import { useRef, useState } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Derp } from "~/api/queries.ts";
import {
  customRegionIds,
  firstError,
  serverDraft,
  serverFieldErrors,
  serverFromDraft,
  withServer,
} from "~/components/derp/model.ts";
import type { ServerDraft } from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { focusFirstInvalid } from "~/components/derp/region-form.tsx";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { Code } from "~/components/ui/code.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { Note, Status } from "~/components/ui/status.tsx";
import { toast } from "~/components/ui/toast.ts";

function Muted({ children }: { readonly children: string }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
}

/**
 * A fact that runs onto a second line rather than being cut off. The list truncates by default, so
 * a long region name or a warning has no room on a phone.
 */
function Wrapping({ children }: { readonly children: ReactNode }): ReactElement {
  return <span className="whitespace-normal">{children}</span>;
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
      value: (
        <Wrapping>
          <span className="flex flex-wrap items-baseline justify-end gap-x-2">
            <span>{regionLabel(server)}</span>
            <Muted>published by the map file, not by these settings</Muted>
          </span>
        </Wrapping>
      ),
    };
  }

  return { label: "Region", value: <Wrapping>{regionLabel(server)}</Wrapping> };
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

/** The relay slopscale runs itself: a switch, its facts and a form for the details. */
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
    <Section
      title="Embedded relay"
      description="The relay this server runs on its own URL, so there is always one next to the control server."
      bodyClassName="p-0"
      {...(canEdit && canRun
        ? {
            actions: (
              <Button
                variant="secondary"
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
            {canRun ? (
              "Serves DERP on the server URL and STUN on its own UDP port. Turning it off moves the machines using it to the next closest relay."
            ) : (
              <>
                The server has no relay key. Set <Code>derp.server.private_key_path</Code> in the
                config file and restart.
              </>
            )}
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
          description="How the relay is published to the machines. Changing the STUN address restarts STUN but not the relay."
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
  const form = useRef<HTMLFormElement>(null);
  const [draft, setDraft] = useState<ServerDraft>(() => serverDraft(settings.server));
  const [touched, setTouched] = useState(false);
  const errors = serverFieldErrors(draft, customRegionIds(settings));
  const issue = firstError(errors);

  function update(patch: Partial<ServerDraft>): void {
    setDraft((current) => ({ ...current, ...patch }));
  }

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      focusFirstInvalid(form.current);

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

  const field = (props: {
    readonly field: keyof ServerDraft;
    readonly label: string;
    readonly placeholder: string;
    readonly description?: string;
  }): ReactElement => (
    <ServerField
      {...props}
      value={String(draft[props.field])}
      error={touched ? errors[props.field] : undefined}
      onChange={(value) => {
        update({ [props.field]: value });
      }}
      onBlur={() => {
        setTouched(true);
      }}
    />
  );

  return (
    <form ref={form} onSubmit={submit} className="flex flex-col gap-4">
      <div className="grid items-start gap-4 sm:grid-cols-2">
        {field({
          field: "regionId",
          label: "Region id",
          placeholder: "999",
          description: "Replaces a fetched region with the same id.",
        })}
        {field({
          field: "regionCode",
          label: "Region code",
          placeholder: "slopscale",
          description: "The short code clients show.",
        })}
      </div>
      {field({
        field: "regionName",
        label: "Region name",
        placeholder: "Slopscale embedded relay",
        description: "Leave empty to use the code.",
      })}
      {field({
        field: "stunAddr",
        label: "STUN address",
        placeholder: "0.0.0.0:3478",
        description: "The UDP host:port STUN listens on. Open it on the firewall.",
      })}
      <div className="grid items-start gap-4 sm:grid-cols-2">
        {field({
          field: "ipv4",
          label: "Public IPv4",
          placeholder: "198.51.100.1",
          description: "Published in the DERP map so machines reach the relay when DNS is down.",
        })}
        {field({
          field: "ipv6",
          label: "Public IPv6",
          placeholder: "2001:db8::1",
          description: "Published in the DERP map so machines reach the relay when DNS is down.",
        })}
      </div>
      <Switch
        label="Admit only this tailnet's machines"
        checked={draft.verifyClients}
        onCheckedChange={(on) => {
          update({ verifyClients: on });
        }}
      />
      <DialogError
        message={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      />
      <FormFooter label="Save" pending={mutations.set.isPending} />
    </form>
  );
}

function ServerField({
  label,
  placeholder,
  description,
  value,
  error,
  onChange,
  onBlur,
}: {
  readonly field: keyof ServerDraft;
  readonly label: string;
  readonly placeholder: string;
  readonly description?: string;
  readonly value: string;
  readonly error: string | undefined;
  readonly onChange: (value: string) => void;
  readonly onBlur: () => void;
}): ReactElement {
  return (
    <Input
      className="w-full"
      label={label}
      value={value}
      placeholder={placeholder}
      spellCheck={false}
      autoComplete="off"
      {...(description === undefined ? {} : { description })}
      {...(error === undefined ? {} : { error })}
      onChange={(event) => {
        onChange(event.target.value);
      }}
      onBlur={onBlur}
    />
  );
}
