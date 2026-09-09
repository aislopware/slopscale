import { DeleteResource } from "@cloudflare/kumo";
import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { App } from "~/api/queries.ts";
import {
  appDomainError,
  appRouteError,
  appValidationError,
  connectorTagError,
  normalizeConnectorTag,
  normalizeDomain,
} from "~/components/apps/model.ts";
import type { AppDraft } from "~/components/apps/model.ts";
import type { AppMutations } from "~/components/apps/mutations.ts";
import { TagInput, usePendingLists } from "~/components/apps/tag-input.tsx";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { Code } from "~/components/ui/code.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface AppDialogProps {
  /** The app to edit; absent when creating one. */
  readonly app?: App | undefined;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AppMutations;
}

/** Creates an app or edits one; the form mounts with the dialog so it starts from the record. */
export function AppDialog(props: AppDialogProps): ReactElement {
  const editing = props.app !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={editing ? "Edit app" : "New app"}
        description="The connectors resolve these domains, advertise a route for every address they learn and forward the traffic."
      >
        <AppForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

function draftFrom(app: App | undefined): AppDraft {
  return {
    name: app?.name ?? "",
    description: app?.description ?? "",
    domains: app?.domains ?? [],
    connectors: app?.connectors ?? [],
    routes: app?.routes ?? [],
  };
}

function AppForm({ app, onOpenChange, mutations }: Omit<AppDialogProps, "open">): ReactElement {
  const [draft, setDraft] = useState<AppDraft>(() => draftFrom(app));
  const lists = usePendingLists();
  const mutation = app === undefined ? mutations.create : mutations.update;
  const update = (patch: Partial<AppDraft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };
  // An entry typed but not yet a chip counts as incomplete: submitting would drop it, and an empty
  // connector list means every connector rather than the one the operator can see.
  const incomplete = appValidationError(draft) !== null || lists.pending;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the buttons say, so the guard is here too.
    if (incomplete || mutation.isPending) {
      return;
    }

    const body = {
      name: draft.name.trim(),
      description: draft.description.trim(),
      domains: [...draft.domains],
      connectors: [...draft.connectors],
      routes: [...draft.routes],
    };

    if (app === undefined) {
      mutations.create.mutate(
        { body },
        {
          onSuccess: () => {
            toast.success("App created");
            onOpenChange(false);
          },
        },
      );
    } else {
      mutations.update.mutate(
        { params: { path: { id: app.id } }, body },
        {
          onSuccess: () => {
            toast.success("App updated");
            onOpenChange(false);
          },
        },
      );
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        value={draft.name}
        placeholder="crm"
        spellCheck={false}
        autoComplete="off"
        onChange={(event) => {
          update({ name: event.target.value });
        }}
      />
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="Sales CRM behind the office firewall"
        onChange={(event) => {
          update({ description: event.target.value });
        }}
      />
      <TagInput
        label="Domains"
        description="example.com or *.example.com. Every address one of these resolves to becomes a route."
        placeholder="crm.example.com"
        value={draft.domains}
        onPendingChange={lists.track("domains")}
        normalize={normalizeDomain}
        validate={appDomainError}
        onValueChange={(domains) => {
          update({ domains });
        }}
      />
      <TagInput
        label="Connectors"
        description="The tags of the machines that serve the app. Leave empty for every connector."
        placeholder="tag:connector"
        value={draft.connectors}
        onPendingChange={lists.track("connectors")}
        normalize={normalizeConnectorTag}
        validate={connectorTagError}
        onValueChange={(connectors) => {
          update({ connectors });
        }}
      />
      <TagInput
        label="Routes"
        description="CIDRs the connectors advertise whatever the domains resolve to. No default routes."
        placeholder="10.0.0.0/24"
        value={draft.routes}
        onPendingChange={lists.track("routes")}
        validate={appRouteError}
        onValueChange={(routes) => {
          update({ routes });
        }}
      />
      <p className="text-kumo-subtle">
        A machine serves the app once it carries one of these tags and runs{" "}
        <Code>tailscale set --advertise-connector</Code>.
      </p>
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={app === undefined ? "Create app" : "Save"}
        pending={mutation.isPending}
        disabled={incomplete}
      />
    </form>
  );
}

export function DeleteAppDialog({
  app,
  open,
  onOpenChange,
  mutations,
}: {
  readonly app: App;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AppMutations;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="app"
      resourceName={app.name}
      deleteButtonText="Delete app"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: app.id } } },
          {
            onSuccess: () => {
              onOpenChange(false);
            },
          },
        );
      }}
    />
  );
}
