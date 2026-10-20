import { Input } from "@cloudflare/kumo/components/input";
import { useNavigate } from "@tanstack/react-router";
import { useId, useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Group, Node, User } from "~/api/queries.ts";
import { DeleteGroupDialog } from "~/components/access/group-dialogs.tsx";
import { isSynced } from "~/components/access/model.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { nodeItems, userItems } from "~/components/access/pickers.ts";
import { EditorPage, FieldGrid, useDraft } from "~/components/policy/editor-page.tsx";
import {
  ConditionRow,
  LogicBadge,
  SettingToggle,
  conditionLabel,
} from "~/components/policy/rule-builder.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

const nameHint = "Letters, digits, spaces, dots, dashes and underscores, and unique.";

interface Draft {
  readonly name: string;
  readonly description: string;
  readonly userIds: readonly string[];
  readonly nodeIds: readonly string[];
  readonly requestable: boolean;
}

function draftFrom(group: Group | undefined): Draft {
  return {
    name: group?.name ?? "",
    description: group?.description ?? "",
    userIds: group?.userIds ?? [],
    nodeIds: group?.nodeIds ?? [],
    requestable: group?.requestable ?? false,
  };
}

type Change = (patch: Partial<Draft>) => void;

function BasicSection({
  draft,
  synced,
  nameError,
  onChange,
}: {
  readonly draft: Draft;
  readonly synced: boolean;
  readonly nameError: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  return (
    <Section title="Basic information" bodyClassName="p-4">
      <FieldGrid>
        <Input
          label="Group name"
          description={
            synced ? "The name comes from the identity provider's groups claim." : nameHint
          }
          value={draft.name}
          readOnly={synced}
          spellCheck={false}
          autoComplete="off"
          placeholder="Engineering"
          {...fieldError(nameError)}
          onChange={(event) => {
            onChange({ name: event.target.value });
          }}
        />
        <Input
          label="Description"
          required={false}
          value={draft.description}
          placeholder="Laptops of the engineering team"
          onChange={(event) => {
            onChange({ description: event.target.value });
          }}
        />
      </FieldGrid>
    </Section>
  );
}

/** Who is in the group, as an Access policy's Include block: any one line makes a member. */
function MembersSection({
  draft,
  synced,
  nodes,
  users,
  onChange,
}: {
  readonly draft: Draft;
  readonly synced: boolean;
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly onChange: Change;
}): ReactElement {
  return (
    <Section
      title="Members"
      description="Rules and networks name the group, so this is the one place to change who they cover."
      bodyClassName="flex flex-col gap-4 p-4"
    >
      <div className="flex items-start gap-3">
        <span className="flex h-lh items-center">
          <LogicBadge logic="or" />
        </span>
        <div className="flex flex-col gap-0.5">
          <h3 className="font-medium text-kumo-default">Include</h3>
          <p className="text-sm text-kumo-subtle">
            A machine is a member when it meets any one of these.
          </p>
        </div>
      </div>
      <div className="flex flex-col gap-3 border-l border-kumo-line pl-4 sm:ml-4">
        <ConditionRow>
          {synced ? (
            <SyncedUsers ids={draft.userIds} users={users} />
          ) : (
            <MultiPicker
              triggerClassName="bg-kumo-control"
              label={conditionLabel("Owner", "is one of")}
              placeholder="Users whose machines are members…"
              items={userItems(users)}
              value={draft.userIds}
              empty="No user matches."
              onValueChange={(userIds) => {
                onChange({ userIds });
              }}
            />
          )}
          <p className="mt-2 text-sm text-kumo-subtle">
            {synced
              ? "Users follow the identity provider's groups claim at each sign-in."
              : "Every machine these users own is a member, including ones they add later."}
          </p>
        </ConditionRow>
        <ConditionRow>
          <MultiPicker
            triggerClassName="bg-kumo-control"
            label={conditionLabel("Machine", "is one of")}
            placeholder="Machines added on their own…"
            items={nodeItems(nodes)}
            value={draft.nodeIds}
            empty="No machine matches."
            onValueChange={(nodeIds) => {
              onChange({ nodeIds });
            }}
          />
          <p className="mt-2 text-sm text-kumo-subtle">
            Machines added on their own, such as tagged servers.
          </p>
        </ConditionRow>
      </div>
    </Section>
  );
}

export interface GroupEditorProps {
  /** The group to edit; absent when creating one. */
  readonly group?: Group | undefined;
  readonly rules: readonly AccessRule[];
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly canEdit: boolean;
}

/**
 * A group on a page of its own, laid out like the dashboard's Access policy rules: the name, then
 * an "Include" block under a purple OR (a machine is in the group when any line holds: one of these
 * users owns it, or it is listed itself), whether users may ask to join, and the save bar at the
 * foot of the window.
 */
export function GroupEditor({
  group,
  rules,
  nodes,
  users,
  canEdit,
}: GroupEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useAccessMutations();
  const mutation = group === undefined ? mutations.createGroup : mutations.updateGroup;
  const synced = group !== undefined && isSynced(group);
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(group), mutation);
  const [deleting, setDeleting] = useState(false);
  const nameError = submitted && draft.name.trim() === "" ? "Name is required." : undefined;
  const toList = (): void => {
    void navigate({ to: "/policy/groups", ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();

    if (draft.name.trim() === "") {
      return false;
    }

    // A synced group's users follow the identity provider, so the request leaves them out rather
    // than resubmitting a list a sign-in may have changed since the page opened.
    const body = {
      name: draft.name.trim(),
      description: draft.description.trim(),
      nodeIds: [...draft.nodeIds],
      ...(synced ? {} : { userIds: [...draft.userIds] }),
      requestable: draft.requestable,
    };
    const done = {
      onSuccess: (): void => {
        toast.success(group === undefined ? "Group created" : "Group saved");
        toList();
      },
    };

    if (group === undefined) {
      mutations.createGroup.mutate({ body }, done);
    } else {
      mutations.updateGroup.mutate({ params: { path: { id: group.id } }, body }, done);
    }

    return true;
  }

  return (
    <EditorPage
      noun="group"
      creating={group === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      onCancel={() => {
        void navigate({ to: "/policy/groups" });
      }}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        group === undefined ? null : (
          <DeleteGroupDialog
            group={group}
            rules={rules}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <BasicSection draft={draft} synced={synced} nameError={nameError} onChange={update} />
      <MembersSection draft={draft} synced={synced} nodes={nodes} users={users} onChange={update} />
      <Section title="Requests" bodyClassName="p-4">
        <SettingToggle
          title="Users may request access"
          description="A signed-in user can ask to join this group for a while. An approver decides under Requests."
          checked={draft.requestable}
          onCheckedChange={(requestable) => {
            update({ requestable });
          }}
        />
      </Section>
    </EditorPage>
  );
}

/** A synced group's users, which the identity provider sets: chips nobody can remove here. */
function SyncedUsers({
  ids,
  users,
}: {
  readonly ids: readonly string[];
  readonly users: readonly User[];
}): ReactElement {
  const items = userItems(users);
  const labelId = useId();

  return (
    <div className="flex flex-col gap-2">
      <span id={labelId} className="font-medium text-kumo-default">
        {conditionLabel("Owner", "is one of")}
      </span>
      <ul
        aria-labelledby={labelId}
        className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-lg bg-kumo-recessed px-2 py-1.5 ring ring-kumo-line"
      >
        {ids.length === 0 ? (
          <li className="px-1 text-kumo-subtle">Nobody has signed in with this group yet</li>
        ) : (
          ids.map((id) => (
            <li
              key={id}
              className="flex h-6 items-center rounded-sm bg-kumo-overlay px-2 text-sm ring-1 ring-kumo-hairline"
            >
              {items.find((item) => item.value === id)?.label ?? `#${id}`}
            </li>
          ))
        )}
      </ul>
    </div>
  );
}
