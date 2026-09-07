import { DeleteResource } from "@cloudflare/kumo";
import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group, Network, Node } from "~/api/queries.ts";
import type { NetworkRequestBody } from "~/api/schema.gen.ts";
import { hasPorts, portsError, toProtocol } from "~/components/access/model.ts";
import type { Protocol } from "~/components/access/model.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { ProtocolFields } from "~/components/access/protocol-fields.tsx";
import { parseList } from "~/components/dns/model.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { prefixesError } from "~/components/networks/model.ts";
import type { NetworkMutations } from "~/components/networks/mutations.ts";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import type { PickerItem } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";
import { isExitRoute, nodeName } from "~/lib/node.ts";

export interface NetworkDialogProps {
  /** The network to edit; absent when creating one. */
  readonly network?: Network | undefined;
  readonly groups: readonly Group[];
  readonly nodes: readonly Node[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: NetworkMutations;
}

/** Creates a network or edits one; the form mounts with the dialog so it starts from the record. */
export function NetworkDialog(props: NetworkDialogProps): ReactElement {
  const editing = props.network !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={editing ? "Edit network" : "New network"}
        description="Prefixes reached through routing machines. The routes are approved on the routers and handed only to the machines in the groups."
      >
        <NetworkForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

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

/** Why the draft cannot be saved yet, or null when it can. */
function draftIssue(draft: Draft): string | null {
  const prefixes = parseList(draft.prefixes);

  if (draft.name.trim() === "" || prefixes.length === 0 || draft.groups.length === 0) {
    return "incomplete";
  }

  return prefixesError(prefixes) ?? (hasPorts(draft.protocol) ? portsError(draft.ports) : null);
}

/** Machines that advertise something come first, with what they advertise as the hint. */
function routerItems(nodes: readonly Node[]): PickerItem[] {
  return nodes
    .map((node) => ({
      value: node.id,
      label: nodeName(node),
      hint:
        node.availableRoutes.length === 0
          ? "Advertises nothing yet"
          : node.availableRoutes
              .filter((route) => !isExitRoute(route) || route === "0.0.0.0/0")
              .map((route) => (isExitRoute(route) ? "exit node" : route))
              .join(", "),
    }))
    .toSorted((left, right) => {
      const advertising =
        Number(right.hint !== "Advertises nothing yet") -
        Number(left.hint !== "Advertises nothing yet");

      return advertising === 0 ? left.label.localeCompare(right.label) : advertising;
    });
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

function NetworkForm({
  network,
  groups,
  nodes,
  onOpenChange,
  mutations,
}: Omit<NetworkDialogProps, "open">): ReactElement {
  const [draft, setDraft] = useState<Draft>(() => draftFrom(network));
  const [touched, setTouched] = useState(false);
  const mutation = network === undefined ? mutations.create : mutations.update;
  const prefixIssue = prefixesError(parseList(draft.prefixes));
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    const body = bodyFrom(draft);
    const done = {
      onSuccess: (): void => {
        toast.success(network === undefined ? "Network created" : "Network updated");
        onOpenChange(false);
      },
    };

    if (network === undefined) {
      mutations.create.mutate({ body }, done);
    } else {
      mutations.update.mutate({ params: { path: { id: network.id } }, body }, done);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        value={draft.name}
        spellCheck={false}
        autoComplete="off"
        placeholder="Office LAN"
        onChange={(event) => {
          update({ name: event.target.value });
        }}
      />
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="Printers and the NAS behind the office router"
        onChange={(event) => {
          update({ description: event.target.value });
        }}
      />
      <Textarea
        label="Prefixes"
        description="One per line: a CIDR or an address. 0.0.0.0/0 makes the network an exit node offer."
        value={draft.prefixes}
        placeholder={"10.10.0.0/24\n192.168.1.0/24"}
        spellCheck={false}
        autoResize
        minRows={2}
        maxRows={8}
        onChange={(event) => {
          update({ prefixes: event.target.value });
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && prefixIssue !== null ? { error: prefixIssue } : {})}
      />
      <MultiPicker
        label="Routers"
        description="Machines that route the prefixes. Each must advertise them with tailscale set --advertise-routes; two or more make a failover pair."
        placeholder="Machines that route the prefixes…"
        items={routerItems(nodes)}
        value={draft.routers}
        onValueChange={(routers) => {
          update({ routers });
        }}
        empty="No machine matches."
      />
      <MultiPicker
        label="Handed to"
        description="Only the machines in these groups get the routes. Pick the builtin All for everyone."
        placeholder="Groups that receive the routes…"
        items={groupItems(groups)}
        value={draft.groups}
        onValueChange={(chosen) => {
          update({ groups: chosen });
        }}
        empty="No group matches."
      />
      <ProtocolFields draft={draft} onChange={update} />
      <Switch
        checked={draft.enabled}
        onCheckedChange={(enabled) => {
          update({ enabled });
        }}
        label={
          <span className="flex flex-col gap-0.5">
            <span className="font-medium text-kumo-default">Enabled</span>
            <span className="text-xs text-kumo-subtle">
              A disabled network keeps its settings but approves and hands out nothing.
            </span>
          </span>
        }
      />
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={network === undefined ? "Create network" : "Save"}
        pending={mutation.isPending}
        disabled={draftIssue(draft) !== null}
      />
    </form>
  );
}

export function DeleteNetworkDialog({
  network,
  open,
  onOpenChange,
  mutations,
}: {
  readonly network: Network;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: NetworkMutations;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="network"
      resourceName={network.name}
      deleteButtonText="Delete network"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: network.id } } },
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
