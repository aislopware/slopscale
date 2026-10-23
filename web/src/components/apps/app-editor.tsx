import { Input } from "@cloudflare/kumo/components/input";
import { cn } from "@cloudflare/kumo/utils";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { App, AppNode } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { DeleteAppDialog } from "~/components/apps/app-dialogs.tsx";
import {
  appDomainError,
  appNameError,
  appRouteError,
  connectorsError,
  domainsError,
  normalizeConnectorTag,
  normalizeDomain,
  pendingSearch,
  routesError,
} from "~/components/apps/model.ts";
import type { AppDraft } from "~/components/apps/model.ts";
import { useAppMutations } from "~/components/apps/mutations.ts";
import { plural } from "~/components/overview/plural.ts";
import { rowLinkClass } from "~/components/table/styles.ts";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { Code } from "~/components/ui/code.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { ListField } from "~/components/ui/list-field.tsx";
import { MachineName } from "~/components/ui/machine-name.tsx";
import { Status } from "~/components/ui/status.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";
import { toast } from "~/components/ui/toast.ts";
import { listValues } from "~/lib/list.ts";

const listPath = "/apps";

function draftFrom(app: App | undefined): AppDraft {
  return {
    name: app?.name ?? "",
    description: app?.description ?? "",
    domains: app?.domains ?? [],
    // The wildcard is how the server stores "every connector"; the field shows that as no tag.
    connectors: (app?.connectors ?? []).filter((connector) => connector !== "*"),
    routes: app?.routes ?? [],
  };
}

/** The draft as the request wants it: the lists tidied, the blank rows dropped. */
function bodyFrom(draft: AppDraft): {
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

interface Issues {
  readonly name?: string;
  /** Nothing to reach at all, or a row that is wrong; the rows say which themselves. */
  readonly serves?: string;
  readonly connectors?: string;
}

/** What is wrong with each part, or nothing; the page shows them after the first save. */
function draftIssues(draft: AppDraft): Issues {
  const body = bodyFrom(draft);
  const name = appNameError(body.name);
  const serves =
    body.domains.length === 0 && body.routes.length === 0
      ? "Add at least one domain or route."
      : (domainsError(body.domains) ?? routesError(body.routes));
  const connectors = connectorsError(body.connectors);

  return {
    ...(name === null ? {} : { name: name === "Enter a name." ? "Give the app a name." : name }),
    ...(serves === null ? {} : { serves }),
    ...(connectors === null ? {} : { connectors }),
  };
}

export interface AppEditorProps {
  /** The app to edit; absent when creating one. */
  readonly app?: App | undefined;
  readonly me: Me;
  readonly canEdit: boolean;
}

/**
 * An app on a page of its own, for creating one or editing it: what it is called, the domains and
 * routes it covers, and the connectors that serve it, with the machines serving it now on an
 * existing app. Saving goes back to the list.
 */
export function AppEditor({ app, me, canEdit }: AppEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useAppMutations();
  const mutation = app === undefined ? mutations.create : mutations.update;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(app), mutation);
  const [deleting, setDeleting] = useState(false);
  const issues = draftIssues(draft);
  const shown = submitted ? issues : {};

  const toList = (): void => {
    void navigate({ to: listPath, ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();

    if (Object.keys(issues).length > 0) {
      return false;
    }

    const body = bodyFrom(draft);
    const done = {
      onSuccess: (): void => {
        toast.success(app === undefined ? "App created" : "App saved");
        toList();
      },
    };

    if (app === undefined) {
      mutations.create.mutate({ body }, done);
    } else {
      mutations.update.mutate({ params: { path: { id: app.id } }, body }, done);
    }

    return true;
  }

  return (
    <FormPage
      noun="app"
      creating={app === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      errorTitle={app === undefined ? "The app was not created" : "The app was not saved"}
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={listPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        app === undefined ? null : (
          <DeleteAppDialog
            app={app}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <DetailsSection draft={draft} issue={shown.name} onChange={update} />
      <ServesSection draft={draft} issue={shown.serves} onChange={update} />
      <ConnectorsSection draft={draft} me={me} issue={shown.connectors} onChange={update} />
      {app === undefined ? null : <MachinesSection nodes={app.nodes} />}
    </FormPage>
  );
}

type Change = (patch: Partial<AppDraft>) => void;

function DetailsSection({
  draft,
  issue,
  onChange,
}: {
  readonly draft: AppDraft;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection title="App details">
      <Input
        label="Name"
        className={nameFieldClass}
        value={draft.name}
        placeholder="crm"
        spellCheck={false}
        autoComplete="off"
        {...fieldError(issue)}
        onChange={(event) => {
          onChange({ name: event.target.value });
        }}
      />
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="Sales CRM behind the office firewall"
        onChange={(event) => {
          onChange({ description: event.target.value });
        }}
      />
    </FormSection>
  );
}

/**
 * The domains and the routes share a part because an app needs one or the other: each row says what
 * is wrong with it once it is left, and an app with neither says so on save.
 */
function ServesSection({
  draft,
  issue,
  onChange,
}: {
  readonly draft: AppDraft;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection
      title="Domains and routes"
      info="The connectors resolve each domain and advertise a route for every address they learn. A wildcard covers the names under a domain, not the domain itself. Routes are advertised as they are, for addresses no domain resolves to."
    >
      <ListField
        label="Domains"
        description="example.com or *.example.com."
        placeholder="crm.example.com"
        addLabel="Add domain"
        value={draft.domains}
        normalize={normalizeDomain}
        validate={appDomainError}
        onValueChange={(domains) => {
          onChange({ domains });
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
          onChange({ routes });
        }}
      />
      {issue === undefined ? null : (
        <p role="alert" className="text-sm leading-snug text-kumo-danger">
          {issue}
        </p>
      )}
    </FormSection>
  );
}

function ConnectorsSection({
  draft,
  me,
  issue,
  onChange,
}: {
  readonly draft: AppDraft;
  readonly me: Me;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  const knownTags = useKnownTags(me);

  return (
    <FormSection title="Connectors">
      <TagField
        label="Tags"
        description="Tags of the machines that serve the app. Leave empty for every connector."
        placeholder="tag:connector"
        value={draft.connectors}
        suggestions={knownTags}
        error={issue}
        onValueChange={(connectors) => {
          onChange({ connectors });
        }}
      />
      <p className="text-kumo-subtle">
        A machine serves the app once it carries one of these tags and runs{" "}
        <Code>tailscale set --advertise-connector</Code>.
      </p>
    </FormSection>
  );
}

/**
 * The machines the app's tags pick today, as the list's Machines popover has them: whether each is
 * online and running the connector, and the routes it waits on, linked to where they are approved.
 */
function MachinesSection({ nodes }: { readonly nodes: readonly AppNode[] }): ReactElement {
  return (
    <FormSection
      title="Connector machines"
      info="The machines that carry one of the app's tags. Changes to the tags show here once saved."
      flush
    >
      {nodes.length === 0 ? (
        <p className="p-4 text-kumo-subtle">No machine carries one of the app&rsquo;s tags yet.</p>
      ) : (
        <ul className="divide-y divide-kumo-line">
          {nodes.map((node) => (
            <MachineRow key={node.nodeId} node={node} />
          ))}
        </ul>
      )}
    </FormSection>
  );
}

function MachineRow({ node }: { readonly node: AppNode }): ReactElement {
  return (
    <li className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-1 px-4 py-3">
      <span className="flex min-w-0 flex-col gap-0.5">
        <MachineName
          name={
            <Link
              to="/machines/$nodeId"
              params={{ nodeId: node.nodeId }}
              className={cn("max-w-full truncate", rowLinkClass)}
            >
              {node.name}
            </Link>
          }
          owner={node.owner}
        />
        {node.connector ? null : (
          <span className="text-xs text-kumo-subtle">Not running the connector</span>
        )}
      </span>
      <span className="flex items-center gap-4">
        {node.pending === 0 ? null : (
          <Link to="/routes" search={pendingSearch([node])} className={rowLinkClass}>
            <Status tone="warning">{`${plural(node.pending, "route")} pending`}</Status>
          </Link>
        )}
        <Status tone={node.online ? "success" : "neutral"}>
          {node.online ? "Online" : "Offline"}
        </Status>
      </span>
    </li>
  );
}
