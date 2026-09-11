import { DeleteResource } from "@cloudflare/kumo";
import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { App } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import {
  appDomainError,
  appRouteError,
  appValidationError,
  normalizeConnectorTag,
  normalizeDomain,
} from "~/components/apps/model.ts";
import type { AppDraft } from "~/components/apps/model.ts";
import type { AppMutations } from "~/components/apps/mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { Code } from "~/components/ui/code.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { ListField } from "~/components/ui/list-field.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";
import { toast } from "~/components/ui/toast.ts";
import { listValues } from "~/lib/list.ts";

export interface AppDialogProps {
  /** The app to edit; absent when creating one. */
  readonly app?: App | undefined;
  readonly me: Me;
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
        description="The connectors resolve the domains and route every address they learn."
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

/** The draft as the request wants it: the lists tidied, the blank rows dropped. */
function cleanDraft(draft: AppDraft): {
  readonly name: string;
  readonly description: string;
  readonly domains: string[];
  readonly connectors: string[];
  readonly routes: string[];
} {
  return {
    name: draft.name.trim(),
    description: draft.description.trim(),
    domains: listValues(draft.domains, normalizeDomain),
    connectors: listValues(draft.connectors, normalizeConnectorTag),
    routes: listValues(draft.routes),
  };
}

function AppForm({ app, me, onOpenChange, mutations }: Omit<AppDialogProps, "open">): ReactElement {
  // The lists hold their rows as typed; a wrong row is what holds the form, a blank one is nothing.
  const [draft, setDraft] = useState<AppDraft>(() => draftFrom(app));
  const knownTags = useKnownTags(me);
  const mutation = app === undefined ? mutations.create : mutations.update;
  const update = (patch: Partial<AppDraft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };
  const body = cleanDraft(draft);
  const incomplete = appValidationError(body) !== null;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the buttons say, so the guard is here too.
    if (incomplete || mutation.isPending) {
      return;
    }

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
      <ListField
        label="Domains"
        description="example.com or *.example.com."
        placeholder="crm.example.com"
        addLabel="Add domain"
        value={draft.domains}
        normalize={normalizeDomain}
        validate={appDomainError}
        onValueChange={(domains) => {
          update({ domains });
        }}
      />
      <TagField
        label="Connectors"
        description="Tags of the machines that serve the app. Leave empty for every connector."
        placeholder="tag:connector"
        value={draft.connectors.filter((connector) => connector !== "*")}
        suggestions={knownTags}
        onValueChange={(connectors) => {
          update({ connectors });
        }}
      />
      <ListField
        label="Routes"
        description="CIDRs the connectors always advertise. No default routes."
        placeholder="10.0.0.0/24"
        addLabel="Add route"
        optional
        value={draft.routes}
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
