import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { InputGroup } from "@cloudflare/kumo/components/input-group";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Service } from "~/api/queries.ts";
import { parseList } from "~/components/dns/model.ts";
import {
  portsError,
  serviceLabel,
  serviceName,
  serviceNameIssue,
} from "~/components/services/model.ts";
import { useServiceMutations } from "~/components/services/mutations.ts";
import { DeleteServiceDialog } from "~/components/services/service-dialogs.tsx";
import { Code } from "~/components/ui/code.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { toast } from "~/components/ui/toast.ts";

const listPath = "/services";

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

/** Why the label cannot name a service, as a sentence, or null when it can. */
function nameIssue(label: string): string | null {
  const trimmed = label.trim();

  if (trimmed === "") {
    return "Give the service a name.";
  }

  const issue = serviceNameIssue(trimmed);

  return issue === null ? null : `The name ${issue}.`;
}

interface Issues {
  readonly label?: string;
  readonly ports?: string;
}

/** What is wrong with each field; the name only counts on a new service, the one place it is set. */
function draftIssues(draft: Draft, creating: boolean): Issues {
  const label = creating ? nameIssue(draft.label) : null;
  const ports = portsError(parseList(draft.ports));

  return {
    ...(label === null ? {} : { label }),
    ...(ports === null ? {} : { ports }),
  };
}

export interface ServiceEditorProps {
  /** The service to edit; absent when creating one. */
  readonly service?: Service | undefined;
  readonly canEdit: boolean;
}

/**
 * A service on a page of its own, for creating one or editing what clients are told about it. The
 * name is set once: it carries the service's addresses, so an existing service keeps it. Saving
 * goes to the service's page.
 */
export function ServiceEditor({ service, canEdit }: ServiceEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useServiceMutations();
  const creating = service === undefined;
  const mutation = creating ? mutations.create : mutations.update;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(service), mutation);
  const [deleting, setDeleting] = useState(false);
  // The name and the ports say what is wrong with them once left, and live after that.
  const [left, setLeft] = useState<{ readonly label?: true; readonly ports?: true }>({});
  const issues = draftIssues(draft, creating);
  const label = submitted || left.label === true ? issues.label : undefined;
  const ports = submitted || left.ports === true ? issues.ports : undefined;
  const detailPath = `/services/${serviceLabel(service?.name ?? "")}`;

  const toService = (name: string): void => {
    void navigate({
      to: "/services/$label",
      params: { label: serviceLabel(name) },
      ignoreBlocker: true,
    });
  };

  function save(): boolean {
    markSubmitted();

    if (Object.keys(issues).length > 0) {
      return false;
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
            toService(name);
          },
        },
      );
    } else {
      mutations.update.mutate(
        { params: { path: { name: service.name } }, body },
        {
          onSuccess: () => {
            toast.success("Service saved");
            toService(service.name);
          },
        },
      );
    }

    return true;
  }

  return (
    <FormPage
      noun="service"
      creating={creating}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      errorTitle={creating ? "The service was not created" : "The service was not saved"}
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={creating ? listPath : detailPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        service === undefined ? null : (
          <DeleteServiceDialog
            service={service}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={() => {
              void navigate({ to: listPath, ignoreBlocker: true });
            }}
          />
        )
      }
    >
      <DetailsSection
        draft={draft}
        creating={creating}
        issue={label}
        onChange={update}
        onLeaveName={() => {
          setLeft((current) => ({ ...current, label: true }));
        }}
      />
      <PortsSection
        value={draft.ports}
        issue={ports}
        onChange={update}
        onLeave={() => {
          setLeft((current) => ({ ...current, ports: true }));
        }}
      />
    </FormPage>
  );
}

type Change = (patch: Partial<Draft>) => void;

/** What the service is called and what clients show for it; the name only on a new one. */
function DetailsSection({
  draft,
  creating,
  issue,
  onChange,
  onLeaveName,
}: {
  readonly draft: Draft;
  readonly creating: boolean;
  readonly issue: string | undefined;
  readonly onChange: Change;
  readonly onLeaveName: () => void;
}): ReactElement {
  return (
    <FormSection title="Service details">
      {creating ? (
        <NameField
          value={draft.label}
          issue={issue}
          onChange={(label) => {
            onChange({ label });
          }}
          onBlur={onLeaveName}
        />
      ) : null}
      <Input
        label="Display name"
        required={false}
        className={nameFieldClass}
        value={draft.displayName}
        placeholder="Internal web"
        description="What clients show instead of the name."
        onChange={(event) => {
          onChange({ displayName: event.target.value });
        }}
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
          onChange({ comment: event.target.value });
        }}
      />
      {creating ? (
        <p className="text-kumo-subtle">
          Then run <Code>tailscale serve --service=svc:{draft.label.trim() || "name"}</Code> on a
          tagged machine and approve it on the service&rsquo;s page.
        </p>
      ) : null}
    </FormSection>
  );
}

function PortsSection({
  value,
  issue,
  onChange,
  onLeave,
}: {
  readonly value: string;
  readonly issue: string | undefined;
  readonly onChange: Change;
  readonly onLeave: () => void;
}): ReactElement {
  return (
    <FormSection
      title="Connection"
      optional
      info="The ports clients are told the service answers on. Leave them empty and clients are told whatever the hosts serve."
    >
      <Input
        label="Ports"
        required={false}
        value={value}
        spellCheck={false}
        autoComplete="off"
        placeholder="tcp:443, udp:53-60"
        description="What clients are told the service listens on, separated by commas."
        {...fieldError(issue)}
        onChange={(event) => {
          onChange({ ports: event.target.value });
        }}
        onBlur={onLeave}
      />
    </FormSection>
  );
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
  readonly issue: string | undefined;
  readonly onChange: (value: string) => void;
  readonly onBlur: () => void;
}): ReactElement {
  return (
    <InputGroup
      label="Name"
      className={nameFieldClass}
      description="Lower-case letters, digits and dashes. It becomes the service's name under your base domain, and cannot change later."
      {...(issue === undefined ? {} : { error: { message: issue, match: true as const } })}
    >
      <InputGroup.Addon>
        <span className="font-mono text-kumo-subtle">svc:</span>
      </InputGroup.Addon>
      <InputGroup.Input
        value={value}
        placeholder="web"
        spellCheck={false}
        autoComplete="off"
        aria-invalid={issue === undefined ? undefined : true}
        onChange={(event) => {
          onChange(event.target.value);
        }}
        onBlur={onBlur}
      />
    </InputGroup>
  );
}
