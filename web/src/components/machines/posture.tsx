import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowsClockwiseIcon, PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { invalidate } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { CustomAttribute, NodePosture } from "~/api/schema.gen.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { AttributeDialog, attributeText } from "~/components/machines/attribute-dialog.tsx";
import { MatchedPostures } from "~/components/machines/matched-postures.tsx";
import { Code } from "~/components/ui/code.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { AttributeText } from "~/components/ui/expression-text.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

const iconSize = 14;

/** The node:... attributes worth a line, in display order; the rest show in the policy only. */
const shownAttributes: readonly { key: string; label: string }[] = [
  { key: "node:os", label: "OS" },
  { key: "node:osVersion", label: "OS version" },
  { key: "node:tsVersion", label: "Client version" },
  { key: "node:tsReleaseTrack", label: "Release track" },
  { key: "node:tsAutoUpdate", label: "Auto-update" },
  { key: "node:machine", label: "Architecture" },
  { key: "node:distro", label: "Distribution" },
  { key: "node:deviceModel", label: "Device model" },
  { key: "node:package", label: "Package" },
];

/**
 * What the policy can check about the machine: the attributes derived from what its client reports,
 * the serial numbers the server collected, and the custom attributes an operator set.
 */
export function PostureSection({
  node,
  me,
}: {
  readonly node: Node;
  readonly me: Me;
}): ReactElement | null {
  const readable = can(me, "devices:posture_attributes:read");
  const posture = useQuery({
    ...api.queryOptions("get", "/api/v1/node/{nodeId}/posture", {
      params: { path: { nodeId: node.id } },
    }),
    enabled: readable,
  });

  if (!readable || posture.data === undefined) {
    return null;
  }

  const canEdit = can(me, "devices:posture_attributes");

  return (
    <Section
      title="Device posture"
      description="What postures in access rules and the policy file can check about this machine."
      bodyClassName="p-0"
    >
      <DefinitionList items={derived(posture.data)} columns={2} />
      <Identity node={node} posture={posture.data} canEdit={canEdit} />
      <IntegrationAttributes posture={posture.data} />
      <CustomAttributes node={node} posture={posture.data} canEdit={canEdit} />
      <MatchedPostures node={node} me={me} />
    </Section>
  );
}

/** A derived attribute in words: the policy sees `true`, the operator reads "Yes". */
function derivedText(value: unknown): string {
  if (typeof value === "boolean") {
    return value ? "Yes" : "No";
  }

  return attributeText(value);
}

function derived(posture: NodePosture): Definition[] {
  return shownAttributes.flatMap(({ key, label }) => {
    const value = posture.attributes[key];
    const text = derivedText(value);

    return text === "" ? [] : [{ key, label, value: text, copy: attributeText(value) }];
  });
}

function Identity({
  node,
  posture,
  canEdit,
}: {
  readonly node: Node;
  readonly posture: NodePosture;
  readonly canEdit: boolean;
}): ReactElement {
  const queryClient = useQueryClient();
  const collect = api.useMutation("post", "/api/v1/node/{nodeId}/posture/collect", {
    onSuccess: async () => {
      toast.success("Identity collected");
      await invalidate(queryClient, "/api/v1/node");
    },
    onError: (error) => {
      toast.error("Could not collect the identity", error);
    },
  });

  return (
    <SectionRow className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 flex-col gap-1">
        <span className="font-medium text-kumo-strong">Serial numbers</span>
        <IdentityText posture={posture} />
      </div>
      {canEdit && posture.identityCollectionOn ? (
        <Button
          variant="secondary"
          size="sm"
          loading={collect.isPending}
          disabled={!node.online}
          icon={<ArrowsClockwiseIcon size={iconSize} />}
          onClick={() => {
            collect.mutate({ params: { path: { nodeId: node.id } } });
          }}
        >
          {posture.identity === undefined ? "Collect" : "Refresh"}
        </Button>
      ) : null}
    </SectionRow>
  );
}

function IdentityText({ posture }: { readonly posture: NodePosture }): ReactElement {
  if (!posture.identityCollectionOn) {
    return (
      <span className="text-xs text-kumo-subtle">
        Collection is off. Turn on Collect device identity under Settings › Tailnet.
      </span>
    );
  }

  const { identity } = posture;

  if (identity === undefined) {
    return (
      <span className="text-xs text-kumo-subtle">
        Not collected. The server asks the next time the machine connects.
      </span>
    );
  }

  if (identity.disabled) {
    return (
      <span className="text-xs text-kumo-subtle">
        Asked <RelativeTime value={identity.collectedAt} />. The client has posture checking off (
        <Code>tailscale set --posture-checking=true</Code>).
      </span>
    );
  }

  return (
    <span className="flex flex-wrap items-center gap-2 text-xs text-kumo-subtle">
      {identity.serialNumbers.length === 0 ? (
        "The client found no serial number"
      ) : (
        <span className="font-mono text-kumo-default">{identity.serialNumbers.join(", ")}</span>
      )}
      <span>
        collected <RelativeTime value={identity.collectedAt} />
      </span>
    </span>
  );
}

function IntegrationAttributes({
  posture,
}: {
  readonly posture: NodePosture;
}): ReactElement | null {
  const attributes = posture.integration ?? [];

  if (attributes.length === 0) {
    return null;
  }

  return (
    <>
      <SectionRow className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="font-medium text-kumo-strong">From integrations</span>
          <span className="text-xs text-kumo-subtle">
            Attributes synced from endpoint security and device management services.
          </span>
        </div>
      </SectionRow>
      {attributes.map((attribute) => (
        <IntegrationAttributeRow key={attribute.key} attribute={attribute} />
      ))}
    </>
  );
}

function IntegrationAttributeRow({
  attribute,
}: {
  readonly attribute: CustomAttribute;
}): ReactElement {
  return (
    <SectionRow className="flex flex-wrap items-center justify-between gap-3 py-3">
      <div className="flex min-w-0 flex-col gap-0.5 text-left">
        <AttributeText name={attribute.key} value={attribute.value} />
        <span className="text-xs text-kumo-subtle">
          {attribute.expiresAt === undefined ? (
            "Does not expire"
          ) : (
            <>
              Expires <RelativeTime value={attribute.expiresAt} />
            </>
          )}
          {attribute.comment === undefined || attribute.comment === ""
            ? null
            : ` · ${attribute.comment}`}
        </span>
      </div>
    </SectionRow>
  );
}

function CustomAttributes({
  node,
  posture,
  canEdit,
}: {
  readonly node: Node;
  readonly posture: NodePosture;
  readonly canEdit: boolean;
}): ReactElement {
  const [editing, setEditing] = useState<CustomAttribute | "new" | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const remove = api.useMutation("delete", "/api/v1/node/{nodeId}/attributes/{key}", {
    onSuccess: async () => {
      toast.success("Attribute removed");
      setRemoving(null);
      await invalidate(queryClient, "/api/v1/node");
    },
    onError: (error) => {
      toast.error("Could not remove the attribute", error);
    },
  });

  return (
    <>
      <SectionRow className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="font-medium text-kumo-strong">Custom attributes</span>
          <span className="text-xs text-kumo-subtle">
            Values the policy can check, each with an optional expiry.
          </span>
        </div>
        {canEdit ? (
          <Button
            variant="secondary"
            size="sm"
            icon={<PlusIcon size={iconSize} />}
            onClick={() => {
              setEditing("new");
            }}
          >
            Add
          </Button>
        ) : null}
      </SectionRow>
      {posture.custom.map((attribute) => (
        <AttributeRow
          key={attribute.key}
          attribute={attribute}
          canEdit={canEdit}
          onEdit={() => {
            setEditing(attribute);
          }}
          onRemove={() => {
            setRemoving(attribute.key);
          }}
        />
      ))}
      <AttributeDialog
        node={node}
        attribute={editing === "new" ? null : editing}
        open={editing !== null}
        onOpenChange={(open) => {
          if (!open) {
            setEditing(null);
          }
        }}
      />
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRemoving(null);
          }
        }}
        title="Remove attribute?"
        {...(removing === null ? {} : { description: `${removing} is removed from this machine.` })}
        confirmLabel="Remove"
        loading={remove.isPending}
        onConfirm={() => {
          if (removing !== null) {
            remove.mutate({ params: { path: { nodeId: node.id, key: removing } } });
          }
        }}
      />
    </>
  );
}

/** One custom attribute: the row opens the editor, the menu edits or removes it. */
function AttributeRow({
  attribute,
  canEdit,
  onEdit,
  onRemove,
}: {
  readonly attribute: CustomAttribute;
  readonly canEdit: boolean;
  readonly onEdit: () => void;
  readonly onRemove: () => void;
}): ReactElement {
  return (
    <SectionRow className="flex flex-wrap items-center justify-between gap-3 py-3">
      <button
        type="button"
        disabled={!canEdit}
        className="flex min-w-0 flex-col gap-0.5 text-left disabled:cursor-default"
        onClick={onEdit}
      >
        <AttributeText name={attribute.key} value={attribute.value} />
        <span className="text-xs text-kumo-subtle">
          {attribute.expiresAt === undefined ? (
            "Does not expire"
          ) : (
            <>
              Expires <RelativeTime value={attribute.expiresAt} />
            </>
          )}
          {attribute.comment === undefined || attribute.comment === ""
            ? null
            : ` · ${attribute.comment}`}
        </span>
      </button>
      {canEdit ? (
        <RowMenu label={`Actions for ${attribute.key}`}>
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={onEdit}>
            Edit…
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={onRemove}>
            Remove…
          </DropdownMenu.Item>
        </RowMenu>
      ) : null}
    </SectionRow>
  );
}
