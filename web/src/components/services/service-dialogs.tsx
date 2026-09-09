import { DeleteResource } from "@cloudflare/kumo";
import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { InputGroup } from "@cloudflare/kumo/components/input-group";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Service } from "~/api/queries.ts";
import { parseList } from "~/components/dns/model.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import {
  portsError,
  serviceLabel,
  serviceName,
  serviceNameIssue,
} from "~/components/services/model.ts";
import type { ServiceMutations } from "~/components/services/mutations.ts";
import { Code } from "~/components/ui/code.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface ServiceDialogProps {
  /** The service to edit; absent when creating one. */
  readonly service?: Service | undefined;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: ServiceMutations;
  /** Where to go after a successful create; the list stays put without one. */
  readonly onCreated?: (name: string) => void;
}

/** Creates a service or edits one; the form mounts with the dialog so it starts from the record. */
export function ServiceDialog(props: ServiceDialogProps): ReactElement {
  const editing = props.service !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={editing ? "Edit service" : "New service"}
        description={
          editing
            ? "The name and the addresses stay as they are; everything else is what clients are told about the service."
            : "A name of its own with a pair of tailnet addresses, hosted by the tagged machines you approve."
        }
      >
        <ServiceForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

interface Draft {
  readonly label: string;
  readonly displayName: string;
  readonly comment: string;
  /** The port list as typed: a comma-separated run of `tcp:443`, `udp:53-60` or `tcp:*`. */
  readonly ports: string;
}

function draftFrom(service: Service | undefined): Draft {
  return {
    label: service === undefined ? "" : serviceLabel(service.name),
    displayName: service?.displayName ?? "",
    comment: service?.comment ?? "",
    ports: (service?.ports ?? []).join(", "),
  };
}

/**
 * The label, with the `svc:` every service name carries as a fixed prefix rather than as something
 * to type: the server adds it, and a name typed with one would arrive with two.
 */
function NameField({
  value,
  issue,
  onChange,
  onBlur,
}: {
  readonly value: string;
  /** What is wrong with the label once the field has been touched, or null. */
  readonly issue: string | null;
  readonly onChange: (value: string) => void;
  readonly onBlur: () => void;
}): ReactElement {
  return (
    <InputGroup
      label="Name"
      description="Lower-case letters, digits and dashes. It becomes the service's name under your base domain."
      {...(issue === null ? {} : { error: { message: issue, match: true as const } })}
    >
      <InputGroup.Addon>
        <span className="font-mono text-kumo-subtle">svc:</span>
      </InputGroup.Addon>
      <InputGroup.Input
        value={value}
        placeholder="web"
        spellCheck={false}
        autoComplete="off"
        onChange={(event) => {
          onChange(event.target.value);
        }}
        onBlur={onBlur}
      />
    </InputGroup>
  );
}

function ServiceForm({
  service,
  onOpenChange,
  mutations,
  onCreated,
}: Omit<ServiceDialogProps, "open">): ReactElement {
  const [draft, setDraft] = useState<Draft>(() => draftFrom(service));
  const [touched, setTouched] = useState(false);
  const mutation = service === undefined ? mutations.create : mutations.update;
  const nameIssue = serviceNameIssue(draft.label.trim());
  const portIssue = portsError(parseList(draft.ports));
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (portIssue !== null || (service === undefined && nameIssue !== null)) {
      return;
    }

    const body = {
      displayName: draft.displayName.trim(),
      comment: draft.comment.trim(),
      ports: parseList(draft.ports),
    };

    if (service === undefined) {
      const name = serviceName(draft.label);

      mutations.create.mutate(
        { body: { ...body, name } },
        {
          onSuccess: () => {
            toast.success("Service created");
            onOpenChange(false);
            onCreated?.(serviceLabel(name));
          },
        },
      );

      return;
    }

    mutations.update.mutate(
      { params: { path: { name: service.name } }, body },
      {
        onSuccess: () => {
          toast.success("Service updated");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      {service === undefined ? (
        <NameField
          value={draft.label}
          issue={touched ? nameIssue : null}
          onChange={(label) => {
            update({ label });
          }}
          onBlur={() => {
            setTouched(true);
          }}
        />
      ) : null}
      <Input
        label="Display name"
        required={false}
        value={draft.displayName}
        placeholder="Internal web"
        description="What clients show instead of the name."
        onChange={(event) => {
          update({ displayName: event.target.value });
        }}
      />
      <Input
        label="Ports"
        required={false}
        value={draft.ports}
        spellCheck={false}
        autoComplete="off"
        placeholder="tcp:443, udp:53-60"
        description="What clients are told the service listens on, separated by commas. Leave it empty for whatever the hosts serve."
        onChange={(event) => {
          update({ ports: event.target.value });
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && portIssue !== null ? { error: portIssue } : {})}
      />
      <Textarea
        label="Comment"
        required={false}
        value={draft.comment}
        placeholder="The internal dashboard, behind the office router."
        autoResize
        minRows={2}
        maxRows={6}
        onChange={(event) => {
          update({ comment: event.target.value });
        }}
      />
      {service === undefined ? (
        <p className="max-w-prose text-kumo-subtle">
          Then run <Code>tailscale serve --service=svc:{draft.label.trim() || "name"}</Code> on a
          tagged machine and approve it here.
        </p>
      ) : null}
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={service === undefined ? "Create service" : "Save"}
        pending={mutation.isPending}
        disabled={service === undefined && draft.label.trim() === ""}
      />
    </form>
  );
}

export function DeleteServiceDialog({
  service,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly service: Service;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: ServiceMutations;
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="service"
      resourceName={serviceLabel(service.name)}
      deleteButtonText="Delete service"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { name: service.name } } },
          {
            onSuccess: () => {
              onOpenChange(false);
              onDeleted?.();
            },
          },
        );
      }}
    />
  );
}
