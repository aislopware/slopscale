import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Derp } from "~/api/queries.ts";
import type { DerpCustomRegion } from "~/api/schema.gen.ts";
import {
  customRegionIds,
  regionCodeError,
  regionIdError,
  relayDraft,
  relayError,
  relayFromDraft,
  withRegion,
} from "~/components/derp/model.ts";
import type { RelayDraft } from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogError } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

const iconSize = 16;

/** Adds a region of relays, or edits one; the whole configuration is sent back. */
export function RegionForm({
  editing,
  derp,
  mutations,
  onDone,
}: {
  readonly editing: DerpCustomRegion | null;
  readonly derp: Derp;
  readonly mutations: DerpMutations;
  readonly onDone: () => void;
}): ReactElement {
  const settings = derp.effective;
  const [id, setId] = useState(editing === null ? "" : String(editing.id));
  const [code, setCode] = useState(editing?.code ?? "");
  const [name, setName] = useState(editing?.name ?? "");
  const [relays, setRelays] = useState<readonly RelayDraft[]>(() =>
    editing === null ? [relayDraft()] : (editing.nodes ?? []).map((relay) => relayDraft(relay)),
  );
  const [touched, setTouched] = useState(false);
  const taken = customRegionIds(settings).filter((existing) => existing !== editing?.id);
  const embeddedId = settings.server.enabled ? [settings.server.regionId ?? 0] : [];
  const issue =
    regionIdError(id.trim(), [...taken, ...embeddedId]) ??
    regionCodeError(code.trim()) ??
    (relays.length === 0 ? "Add at least one relay." : null) ??
    relays.map((relay) => relayError(relay)).find((error) => error !== null) ??
    null;

  function updateRelay(key: string, patch: Partial<RelayDraft>): void {
    setRelays((current) =>
      current.map((relay) => (relay.key === key ? { ...relay, ...patch } : relay)),
    );
  }

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      return;
    }

    const region: DerpCustomRegion = {
      id: Number(id.trim()),
      code: code.trim(),
      nodes: relays.map((relay) => relayFromDraft(relay)),
    };

    if (name.trim() !== "") {
      region.name = name.trim();
    }

    mutations.set.mutate(
      { body: withRegion(settings, region, editing?.id) },
      {
        onSuccess: () => {
          toast.success(editing === null ? "Region added" : "Region updated");
          onDone();
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <RegionFields
        id={id}
        code={code}
        name={name}
        onId={setId}
        onCode={setCode}
        onName={setName}
        onBlur={() => {
          setTouched(true);
        }}
      />
      <div className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <span className="font-medium text-kumo-strong">Relays</span>
          <Button
            variant="ghost"
            size="xs"
            icon={PlusIcon}
            onClick={() => {
              setRelays((current) => [...current, relayDraft()]);
            }}
          >
            Add relay
          </Button>
        </div>
        {relays.map((relay) => (
          <RelayFields
            key={relay.key}
            relay={relay}
            removable={relays.length > 1}
            onChange={(patch) => {
              updateRelay(relay.key, patch);
            }}
            onRemove={() => {
              setRelays((current) => current.filter((other) => other.key !== relay.key));
            }}
            onBlur={() => {
              setTouched(true);
            }}
          />
        ))}
      </div>
      {touched && issue !== null ? <p className="text-sm text-kumo-danger">{issue}</p> : null}
      <DialogError
        message={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      />
      <FormFooter label={editing === null ? "Add" : "Save"} pending={mutations.set.isPending} />
    </form>
  );
}

function RegionFields({
  id,
  code,
  name,
  onId,
  onCode,
  onName,
  onBlur,
}: {
  readonly id: string;
  readonly code: string;
  readonly name: string;
  readonly onId: (value: string) => void;
  readonly onCode: (value: string) => void;
  readonly onName: (value: string) => void;
  readonly onBlur: () => void;
}): ReactElement {
  return (
    <div className="grid items-start gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,2fr)]">
      <Input
        className="w-full"
        label="Region id"
        value={id}
        placeholder="900"
        inputMode="numeric"
        autoComplete="off"
        description="Above 900 stays clear of Tailscale's."
        onChange={(event) => {
          onId(event.target.value);
        }}
        onBlur={onBlur}
      />
      <Input
        className="w-full"
        label="Code"
        value={code}
        placeholder="sgp"
        spellCheck={false}
        autoComplete="off"
        description="Short code the clients show."
        onChange={(event) => {
          onCode(event.target.value);
        }}
        onBlur={onBlur}
      />
      <Input
        className="w-full"
        label="Name"
        value={name}
        placeholder="Singapore"
        autoComplete="off"
        description="Empty takes the code."
        onChange={(event) => {
          onName(event.target.value);
        }}
      />
    </div>
  );
}

function RelayFields({
  relay,
  removable,
  onChange,
  onRemove,
  onBlur,
}: {
  readonly relay: RelayDraft;
  readonly removable: boolean;
  readonly onChange: (patch: Partial<RelayDraft>) => void;
  readonly onRemove: () => void;
  readonly onBlur: () => void;
}): ReactElement {
  const text = ({
    key,
    label,
    placeholder,
    description,
  }: {
    readonly key: keyof RelayDraft;
    readonly label: string;
    readonly placeholder: string;
    readonly description?: string;
  }): ReactElement => (
    <Input
      className="w-full"
      label={label}
      value={String(relay[key])}
      placeholder={placeholder}
      spellCheck={false}
      autoComplete="off"
      {...(description === undefined ? {} : { description })}
      onChange={(event) => {
        onChange({ [key]: event.target.value });
      }}
      onBlur={onBlur}
    />
  );

  return (
    <fieldset className="flex flex-col gap-3 rounded-lg border border-kumo-line bg-kumo-recessed p-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          {text({
            key: "hostName",
            label: "Host name",
            placeholder: "derp.example.com",
            description: "What its certificate is for.",
          })}
        </div>
        {removable ? (
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            icon={<TrashIcon size={iconSize} />}
            aria-label={`Remove relay ${relay.hostName === "" ? "" : relay.hostName}`.trim()}
            className="mt-6"
            onClick={onRemove}
          />
        ) : null}
      </div>
      <div className="grid items-start gap-3 sm:grid-cols-2">
        {text({
          key: "ipv4",
          label: "IPv4",
          placeholder: "203.0.113.5",
          description: "Fixed address, or none.",
        })}
        {text({
          key: "ipv6",
          label: "IPv6",
          placeholder: "2001:db8::5",
          description: "Fixed address, or none.",
        })}
      </div>
      <div className="grid items-start gap-3 sm:grid-cols-3">
        {text({ key: "derpPort", label: "HTTPS port", placeholder: "443" })}
        {text({ key: "stunPort", label: "STUN port", placeholder: "3478" })}
        {text({
          key: "name",
          label: "Name",
          placeholder: relay.hostName === "" ? "derp1" : relay.hostName,
          description: "Empty takes the host name.",
        })}
      </div>
      <div className="flex flex-wrap gap-6">
        <Switch
          label="STUN only"
          size="sm"
          checked={relay.stunOnly}
          onCheckedChange={(on) => {
            onChange({ stunOnly: on });
          }}
        />
        <Switch
          label="Serves HTTP on port 80"
          size="sm"
          checked={relay.canPort80}
          onCheckedChange={(on) => {
            onChange({ canPort80: on });
          }}
        />
      </div>
    </fieldset>
  );
}
