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
import { DurationSelect } from "~/components/access/request-dialogs.tsx";
import { durationChoices } from "~/components/access/request-model.ts";
import {
  ConditionRow,
  LogicBadge,
  SettingToggle,
  conditionLabel,
} from "~/components/policy/rule-builder.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FieldGrid, FormPage, FormSection, useDraft } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";
import { formatDuration } from "~/lib/time.ts";

const nameHint = "Letters, digits, spaces, dots, dashes and underscores, and unique.";

interface Draft {
  readonly name: string;
  readonly description: string;
  readonly userIds: readonly string[];
  readonly nodeIds: readonly string[];
  readonly requestable: boolean;
  readonly autoApprove: boolean;
  /** The longest one request may ask for, in seconds; 0 leaves only the server's bounds. */
  readonly maxRequestSeconds: number;
}

function draftFrom(group: Group | undefined): Draft {
  return {
    name: group?.name ?? "",
    description: group?.description ?? "",
    userIds: group?.userIds ?? [],
    nodeIds: group?.nodeIds ?? [],
    requestable: group?.requestable ?? false,
    autoApprove: group?.autoApprove ?? false,
    maxRequestSeconds: group?.maxRequestSeconds ?? 0,
  };
}

/** "No limit", the presets the request page offers, and a cap set elsewhere that is none of them. */
function limitChoices(current: number): { seconds: number; label: string }[] {
  const choices = [{ seconds: 0, label: "No limit" }, ...durationChoices];

  return choices.some((choice) => choice.seconds === current)
    ? choices
    : [...choices, { seconds: current, label: formatDuration(current) }].toSorted(
        (left, right) => left.seconds - right.seconds,
      );
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
    <FormSection title="Group details">
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
    </FormSection>
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
    <FormSection
      title="Members"
      info="Rules and networks name the group, so this is the one place to change who they cover."
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
    </FormSection>
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
      // The update replaces both, so they go with every save; the server refuses approving by
      // itself on a group nobody may request.
      autoApprove: draft.requestable && draft.autoApprove,
      maxRequestSeconds: draft.maxRequestSeconds,
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
    <FormPage
      noun="group"
      creating={group === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo="/policy/groups"
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
      <RequestsSection draft={draft} onChange={update} />
    </FormPage>
  );
}

/** Whether users may ask to join, and when they may, whether asking is enough and for how long. */
function RequestsSection({
  draft,
  onChange,
}: {
  readonly draft: Draft;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection title="Requests">
      <SettingToggle
        title="Users may request access"
        description={
          draft.requestable && draft.autoApprove
            ? "A signed-in user can ask to join this group for a while."
            : "A signed-in user can ask to join this group for a while. An approver decides under Requests."
        }
        checked={draft.requestable}
        onCheckedChange={(requestable) => {
          onChange(requestable ? { requestable } : { requestable, autoApprove: false });
        }}
      >
        {draft.requestable ? (
          <div className="max-w-md">
            <DurationSelect
              label="Longest request"
              description="One request may ask for at most this long."
              choices={limitChoices(draft.maxRequestSeconds)}
              value={draft.maxRequestSeconds}
              onValueChange={(maxRequestSeconds) => {
                onChange({ maxRequestSeconds });
              }}
            />
          </div>
        ) : null}
      </SettingToggle>
      {draft.requestable ? (
        <SettingToggle
          title="Approve requests automatically"
          description="Requests are granted as soon as they are filed, without an approver."
          checked={draft.autoApprove}
          onCheckedChange={(autoApprove) => {
            onChange({ autoApprove });
          }}
        />
      ) : null}
    </FormSection>
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
