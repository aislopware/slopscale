import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group, Network, Node } from "~/api/queries.ts";
import type { NetworkRequestBody } from "~/api/schema.gen.ts";
import { hasPorts, isNarrowed, portsError, toProtocol } from "~/components/access/model.ts";
import type { Protocol } from "~/components/access/model.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { ProtocolFields } from "~/components/access/protocol-fields.tsx";
import { parseList } from "~/components/dns/model.ts";
import { prefixesError } from "~/components/networks/model.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { DeleteNetworkDialog } from "~/components/networks/network-dialogs.tsx";
import { SettingToggle } from "~/components/policy/rule-builder.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { Code } from "~/components/ui/code.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import type { PickerItem } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";
import { isExitRoute, nodeLabel, nodeName, ownerLabel } from "~/lib/node.ts";

const listPath = "/networks";

interface Draft {
  readonly name: string;
  readonly description: string;
  readonly prefixes: string;
  readonly routers: readonly string[];
  readonly groups: readonly string[];
  readonly protocol: Protocol;
  readonly ports: string;
  readonly enabled: boolean;
}

function draftFrom(network: Network | undefined): Draft {
  return {
    name: network?.name ?? "",
    description: network?.description ?? "",
    prefixes: (network?.prefixes ?? []).join("\n"),
    routers: network?.routerNodeIds ?? [],
    groups: network?.groupIds ?? [],
    protocol: toProtocol(network?.protocol ?? "all"),
    ports: network?.ports ?? "",
    enabled: network?.enabled ?? true,
  };
}

/** What is wrong with each field, or nothing; the page shows them after the first save. */
function draftIssues(draft: Draft): {
  readonly name?: string;
  readonly prefixes?: string;
  readonly groups?: string;
  readonly ports?: string;
} {
  const prefixes = parseList(draft.prefixes);
  const prefixIssue = prefixes.length === 0 ? "Add at least one prefix." : prefixesError(prefixes);
  const portIssue = hasPorts(draft.protocol) ? portsError(draft.ports) : null;

  return {
    ...(draft.name.trim() === "" ? { name: "Give the network a name." } : {}),
    ...(prefixIssue === null ? {} : { prefixes: prefixIssue }),
    ...(draft.groups.length === 0 ? { groups: "Pick the groups that get the routes." } : {}),
    ...(portIssue === null ? {} : { ports: portIssue }),
  };
}

/** Machines that advertise something come first, with whose they are and what they advertise. */
function routerItems(nodes: readonly Node[]): PickerItem[] {
  return nodes
    .toSorted((left, right) => {
      const advertising =
        Number(right.availableRoutes.length > 0) - Number(left.availableRoutes.length > 0);

      return advertising === 0 ? nodeName(left).localeCompare(nodeName(right)) : advertising;
    })
    .map((node) => ({
      value: node.id,
      label: nodeName(node),
      hint: `${ownerLabel(node)} · ${
        node.availableRoutes.length === 0
          ? "advertises nothing"
          : node.availableRoutes
              .filter((route) => !isExitRoute(route) || route === "0.0.0.0/0")
              .map((route) => (isExitRoute(route) ? "exit node" : route))
              .join(", ")
      }`,
      chip: nodeLabel(node),
    }));
}

/** The request body for the draft, trimmed the way the server stores it. */
function bodyFrom(draft: Draft): NetworkRequestBody {
  return {
    name: draft.name.trim(),
    description: draft.description.trim(),
    enabled: draft.enabled,
    prefixes: parseList(draft.prefixes),
    routerNodeIds: [...draft.routers],
    groupIds: [...draft.groups],
    protocol: draft.protocol,
    ports: hasPorts(draft.protocol) ? draft.ports.trim() : "",
  };
}

export interface NetworkEditorProps {
  /** The network to edit; absent when creating one. */
  readonly network?: Network | undefined;
  readonly groups: readonly Group[];
  readonly nodes: readonly Node[];
  /** Whether the tailnet has a packet filter; without one the protocol and ports do nothing yet. */
  readonly enforcing: boolean;
  readonly canEdit: boolean;
}

/**
 * A network on a page of its own, for creating one or editing it: what it is called, the prefixes
 * it carries, the machines that route them, and who receives them. Saving goes back to the list.
 */
export function NetworkEditor({
  network,
  groups,
  nodes,
  enforcing,
  canEdit,
}: NetworkEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useNetworkMutations();
  const mutation = network === undefined ? mutations.create : mutations.update;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(network), mutation);
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
        toast.success(network === undefined ? "Network created" : "Network saved");
        toList();
      },
    };

    if (network === undefined) {
      mutations.create.mutate({ body }, done);
    } else {
      mutations.update.mutate({ params: { path: { id: network.id } }, body }, done);
    }

    return true;
  }

  return (
    <FormPage
      noun="network"
      creating={network === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      errorTitle={
        network === undefined ? "The network was not created" : "The network was not saved"
      }
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={listPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        network === undefined ? null : (
          <DeleteNetworkDialog
            network={network}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <DetailsSection draft={draft} issues={shown} onChange={update} />
      <PrefixesSection draft={draft} issue={shown.prefixes} onChange={update} />
      <RoutersSection draft={draft} nodes={nodes} onChange={update} />
      <AccessSection
        draft={draft}
        groups={groups}
        enforcing={enforcing}
        issue={shown.groups}
        onChange={update}
      />
      <FormSection title="Status">
        <SettingToggle
          title="Enabled"
          description="A disabled network keeps its settings but approves no routes and gives none out."
          checked={draft.enabled}
          onCheckedChange={(enabled) => {
            update({ enabled });
          }}
        />
      </FormSection>
    </FormPage>
  );
}

type Change = (patch: Partial<Draft>) => void;

function DetailsSection({
  draft,
  issues,
  onChange,
}: {
  readonly draft: Draft;
  readonly issues: { readonly name?: string };
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection title="Network details">
      <Input
        label="Name"
        className={nameFieldClass}
        value={draft.name}
        spellCheck={false}
        autoComplete="off"
        placeholder="Office LAN"
        {...fieldError(issues.name)}
        onChange={(event) => {
          onChange({ name: event.target.value });
        }}
      />
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="Printers and the NAS behind the office router"
        onChange={(event) => {
          onChange({ description: event.target.value });
        }}
      />
    </FormSection>
  );
}

/**
 * The prefixes say what is wrong with them as soon as the field is left, like the dashboard's
 * format checks; an empty list waits for the save.
 */
function PrefixesSection({
  draft,
  issue,
  onChange,
}: {
  readonly draft: Draft;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  const [left, setLeft] = useState(false);
  const format = left ? prefixesError(parseList(draft.prefixes)) : null;

  return (
    <FormSection
      title="Addresses"
      info="The addresses the network reaches. 0.0.0.0/0 makes its routers exit nodes."
    >
      <Textarea
        label="Prefixes"
        description="One per line. A CIDR or an address. Use 0.0.0.0/0 to offer an exit node."
        value={draft.prefixes}
        placeholder={"10.10.0.0/24\n192.168.1.0/24"}
        spellCheck={false}
        autoResize
        minRows={3}
        maxRows={12}
        className="font-mono"
        {...fieldError(issue ?? format ?? undefined)}
        onChange={(event) => {
          onChange({ prefixes: event.target.value });
        }}
        onBlur={() => {
          setLeft(true);
        }}
      />
    </FormSection>
  );
}

function RoutersSection({
  draft,
  nodes,
  onChange,
}: {
  readonly draft: Draft;
  readonly nodes: readonly Node[];
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection
      title="Routers"
      optional
      info="Two or more routers give failover: each machine uses the nearest healthy one."
    >
      <MultiPicker
        label="Machines"
        description={
          <>
            Each must advertise the prefixes with <Code>tailscale set --advertise-routes</Code>.
          </>
        }
        placeholder="Search machines…"
        items={routerItems(nodes)}
        value={draft.routers}
        onValueChange={(routers) => {
          onChange({ routers });
        }}
        empty="No machine matches."
      />
    </FormSection>
  );
}

function AccessSection({
  draft,
  groups,
  enforcing,
  issue,
  onChange,
}: {
  readonly draft: Draft;
  readonly groups: readonly Group[];
  readonly enforcing: boolean;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection
      title="Access"
      info="Only machines in these groups get the routes, limited to the protocol and ports you pick."
    >
      <MultiPicker
        label="Groups"
        description="Pick the built-in All group for everyone."
        placeholder="Search groups…"
        items={groupItems(groups)}
        value={draft.groups}
        error={issue}
        onValueChange={(chosen) => {
          onChange({ groups: chosen });
        }}
        empty="No group matches."
      />
      <ProtocolFields draft={draft} onChange={onChange} />
      <NarrowingNotice draft={draft} enforcing={enforcing} />
    </FormSection>
  );
}

/** Says when the protocol and ports on the form do nothing yet because the tailnet is open. */
function NarrowingNotice({
  draft,
  enforcing,
}: {
  readonly draft: Pick<Draft, "protocol" | "ports">;
  readonly enforcing: boolean;
}): ReactElement | null {
  if (enforcing || !isNarrowed(draft)) {
    return null;
  }

  return (
    <Callout
      tone="warning"
      title="Protocol and ports are not enforced yet"
      description="The tailnet is open, so the groups reach every port behind the routers. Enable an access rule to enforce them."
    />
  );
}
