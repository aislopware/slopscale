import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { useEffect, useId, useRef } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Derp } from "~/api/queries.ts";
import {
  customRegionIds,
  serverDraft,
  serverFieldErrors,
  serverFromDraft,
  withServer,
} from "~/components/derp/model.ts";
import type { ServerDraft } from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { useShown } from "~/components/derp/relay-list.tsx";
import type { Shown } from "~/components/derp/relay-list.tsx";
import { SettingToggle } from "~/components/policy/rule-builder.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { useDraft } from "~/components/ui/form-page.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Moves to the first field that says what is wrong, after React rendered the marks. */
function focusFirstInvalid(form: HTMLFormElement | null): void {
  requestAnimationFrame(() => {
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  });
}

/**
 * The embedded relay's details turned into their fields in the same card, as the dashboard edits a
 * card of settings in place: Cancel and Save in the card's footer, Escape cancels, and a save goes
 * back to the facts.
 */
export function EmbeddedDetailsForm({
  derp,
  mutations,
  onDone,
}: {
  readonly derp: Derp;
  readonly mutations: DerpMutations;
  /** Back to the facts, after a save or a cancel. */
  readonly onDone: () => void;
}): ReactElement {
  const settings = derp.effective;
  const formId = useId();
  const form = useRef<HTMLFormElement>(null);
  const { draft, submitted, update, markSubmitted } = useDraft(
    serverDraft(settings.server),
    mutations.set,
  );
  const shown = useShown(() => submitted);
  const errors = serverFieldErrors(draft, customRegionIds(settings));
  const { reset } = mutations.set;

  // A failure from before the card opened must not greet it.
  useEffect(() => {
    reset();
  }, [reset]);

  useEffect(() => {
    form.current?.querySelector<HTMLElement>("input")?.focus();
  }, []);

  // Escape cancels, from any field of the card.
  useEffect(() => {
    const node = form.current;
    const keyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape" && !event.defaultPrevented) {
        event.preventDefault();
        onDone();
      }
    };

    node?.addEventListener("keydown", keyDown);

    return (): void => {
      node?.removeEventListener("keydown", keyDown);
    };
  }, [onDone]);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    markSubmitted();

    if (Object.keys(errors).length > 0) {
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

  const field = (props: FieldProps): ReactElement => (
    <ServerField
      {...props}
      value={draft[props.field]}
      error={shown.error(props.field, draft[props.field], errors[props.field])}
      onChange={(value) => {
        update({ [props.field]: value });
      }}
      shown={shown}
    />
  );

  return (
    <Section
      title="Details"
      description="How the relay is published to the machines. Changing the STUN address restarts STUN but not the relay."
      footer={
        <>
          <Button variant="secondary" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" form={formId} variant="primary" loading={mutations.set.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        id={formId}
        ref={form}
        noValidate
        aria-label="Embedded relay details"
        onSubmit={submit}
        className="flex flex-col gap-4 p-4"
      >
        <PublishedFields field={field} />
        <StunFields draft={draft} field={field} onChange={update} />
        <SettingToggle
          title="Admit only this tailnet's machines"
          description="Other Tailscale clients that find the relay are turned away."
          checked={draft.verifyClients}
          onCheckedChange={(on) => {
            update({ verifyClients: on });
          }}
        />
        {mutations.set.isError && !mutations.stale ? (
          <Banner
            variant="error"
            title="Not saved"
            description={errorMessage(mutations.set.error)}
          />
        ) : null}
      </form>
    </Section>
  );
}

type TextField = "regionId" | "regionCode" | "regionName" | "stunAddr" | "ipv4" | "ipv6";

interface FieldProps {
  readonly field: TextField;
  readonly label: string;
  readonly placeholder: string;
  readonly description?: string;
  readonly optional?: boolean;
}

/** The region the relay joins and the addresses it is published with. */
function PublishedFields({
  field,
}: {
  readonly field: (props: FieldProps) => ReactElement;
}): ReactElement {
  return (
    <>
      <div className="grid items-start gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,2fr)]">
        {field({ field: "regionId", label: "Region id", placeholder: "999" })}
        {field({ field: "regionCode", label: "Region code", placeholder: "slopscale" })}
        {field({
          field: "regionName",
          label: "Region name",
          placeholder: "Slopscale embedded relay",
          description: "Leave empty to use the code.",
          optional: true,
        })}
      </div>
      <div className="grid items-start gap-4 sm:grid-cols-2">
        {field({
          field: "ipv4",
          label: "Public IPv4",
          placeholder: "198.51.100.1",
          description: "Published so machines reach the relay when DNS is down.",
          optional: true,
        })}
        {field({
          field: "ipv6",
          label: "Public IPv6",
          placeholder: "2001:db8::1",
          description: "Published so machines reach the relay when DNS is down.",
          optional: true,
        })}
      </div>
    </>
  );
}

function StunFields({
  draft,
  field,
  onChange,
}: {
  readonly draft: ServerDraft;
  readonly field: (props: FieldProps) => ReactElement;
  readonly onChange: (patch: Partial<ServerDraft>) => void;
}): ReactElement {
  return (
    <SettingToggle
      title="Answer STUN"
      description="Turn off when machines on the relay's own network reach it through their router and report a hard NAT."
      checked={draft.stunEnabled}
      onCheckedChange={(on) => {
        onChange({ stunEnabled: on });
      }}
    >
      {draft.stunEnabled
        ? field({
            field: "stunAddr",
            label: "STUN address",
            placeholder: "0.0.0.0:3478",
            description: "The UDP host:port STUN listens on. Open it on the firewall.",
          })
        : null}
    </SettingToggle>
  );
}

function ServerField({
  field,
  label,
  placeholder,
  description,
  optional = false,
  value,
  error,
  shown,
  onChange,
}: FieldProps & {
  readonly value: string;
  readonly error: string | undefined;
  readonly shown: Shown;
  readonly onChange: (value: string) => void;
}): ReactElement {
  return (
    <Input
      className="w-full"
      label={label}
      value={value}
      placeholder={placeholder}
      spellCheck={false}
      autoComplete="off"
      {...(optional ? { required: false } : {})}
      {...(description === undefined ? {} : { description })}
      {...fieldError(error)}
      onChange={(event) => {
        onChange(event.target.value);
      }}
      onBlur={() => {
        shown.leave(field);
      }}
    />
  );
}
