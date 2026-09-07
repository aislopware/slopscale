import { Button } from "@cloudflare/kumo/components/button";
import { Input, InputArea } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import { groupsQuery, usersQuery } from "~/api/queries.ts";
import type { Group, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { CreatedKey, useCreatedKey } from "~/components/keys/created-key.tsx";
import { expirationFor, expiryOptions } from "~/components/keys/expiration.ts";
import type { ExpiryChoice } from "~/components/keys/expiration.ts";
import { usePreAuthKeyMutations } from "~/components/keys/mutations.ts";
import { connectCommand } from "~/components/machines/connect.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { userLabel } from "~/lib/node.ts";

interface Draft {
  readonly userId: string;
  readonly expiry: ExpiryChoice;
  readonly reusable: boolean;
  readonly ephemeral: boolean;
  readonly preauthorized: boolean;
  readonly tags: string;
  readonly groupIds: readonly string[];
}

const emptyDraft: Draft = {
  userId: "",
  expiry: "7d",
  reusable: false,
  ephemeral: false,
  preauthorized: true,
  tags: "",
  groupIds: [],
};

const revealNote = "The full key is shown this once and cannot be read again.";
const tagRows = 2;

/** Splits the textarea into tags, adding the `tag:` prefix the policy expects. */
function parseTags(text: string): string[] {
  return text
    .split(/[\s,]+/u)
    .map((tag) => tag.trim())
    .filter((tag) => tag !== "")
    .map((tag) => (tag.startsWith("tag:") ? tag : `tag:${tag}`));
}

/**
 * What the caller came for. The keys page mints a key; everywhere else the key is a means to an end
 * and the dialog says so, down to the command that uses it.
 */
export type PreAuthKeyIntent = "key" | "add-machine";

const titles: Record<PreAuthKeyIntent, { readonly form: string; readonly created: string }> = {
  key: { form: "Create pre-auth key", created: "Key created" },
  "add-machine": { form: "Add machine", created: "Add machine" },
};

const descriptions: Record<PreAuthKeyIntent, string> = {
  key: "A machine registers with the key instead of signing in, so it belongs to the user you pick.",
  "add-machine":
    "A machine joins by registering itself with a pre-auth key, so it belongs to the user you pick.",
};

export function CreatePreAuthKeyDialog({
  me,
  open,
  onOpenChange,
  intent = "key",
}: {
  readonly me: Me;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly intent?: PreAuthKeyIntent;
}): ReactElement {
  const [created, setCreated] = useCreatedKey(open);
  const close = (): void => {
    onOpenChange(false);
  };

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="lg"
        title={created === null ? titles[intent].form : titles[intent].created}
        description={created === null ? descriptions[intent] : undefined}
      >
        {created === null ? (
          <CreatePreAuthKeyForm me={me} onCreated={setCreated} />
        ) : (
          <CreatedKey
            value={created}
            note={revealNote}
            {...(intent === "add-machine" ? { command: connectCommand(created) } : {})}
            onDone={close}
          />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

function CreatePreAuthKeyForm({
  me,
  onCreated,
}: {
  readonly me: Me;
  readonly onCreated: (key: string) => void;
}): ReactElement {
  const mayListUsers = can(me, "users:read");
  const users = useQuery({ ...usersQuery, enabled: mayListUsers });
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const { create } = usePreAuthKeyMutations();
  const [draft, setDraft] = useState<Draft>({ ...emptyDraft, userId: me.user?.id ?? "" });
  const userList = users.data?.users ?? [];
  const userId = draft.userId === "" ? (userList[0]?.id ?? "") : draft.userId;

  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    create.mutate(
      {
        body: {
          user: userId,
          reusable: draft.reusable,
          ephemeral: draft.ephemeral,
          preauthorized: draft.preauthorized,
          expiration: expirationFor(draft.expiry),
          aclTags: parseTags(draft.tags),
          groupIds: [...draft.groupIds],
        },
      },
      {
        onSuccess: (data) => {
          onCreated(data.preAuthKey.key);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <PreAuthKeyFields
        draft={{ ...draft, userId }}
        users={mayListUsers ? userList : undefined}
        groups={groups.data?.groups}
        onChange={update}
      />
      <DialogError message={create.isError ? errorMessage(create.error) : undefined} />
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button type="submit" variant="primary" loading={create.isPending} disabled={userId === ""}>
          Create key
        </Button>
      </DialogFooter>
    </form>
  );
}

function PreAuthKeyFields({
  draft,
  users,
  groups,
  onChange,
}: {
  readonly draft: Draft;
  /** The users to choose from, or undefined when the caller may not list them. */
  readonly users: readonly User[] | undefined;
  /** The groups a registered machine may join, or undefined when the caller may not list them. */
  readonly groups: readonly Group[] | undefined;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  return (
    <>
      {users === undefined ? (
        <Input
          label="User id"
          description="The numeric id of the user the key belongs to."
          value={draft.userId}
          spellCheck={false}
          onChange={(event) => {
            onChange({ userId: event.target.value });
          }}
        />
      ) : (
        <UserSelect
          users={users}
          value={draft.userId}
          onChange={(userId) => {
            onChange({ userId });
          }}
        />
      )}
      <Select
        className="w-full"
        label="Expiration"
        value={draft.expiry}
        items={expiryOptions}
        onValueChange={(expiry: ExpiryChoice | null) => {
          if (expiry !== null) {
            onChange({ expiry });
          }
        }}
      />
      <Switch.Group>
        <Switch.Legend>Options</Switch.Legend>
        <OptionSwitch
          label="Reusable"
          description="Registers more than one machine with the same key."
          checked={draft.reusable}
          onChange={(reusable) => {
            onChange({ reusable });
          }}
        />
        <OptionSwitch
          label="Ephemeral"
          description="The machine is removed when it goes offline."
          checked={draft.ephemeral}
          onChange={(ephemeral) => {
            onChange({ ephemeral });
          }}
        />
        <OptionSwitch
          label="Pre-authorized"
          description="The machine skips device approval."
          checked={draft.preauthorized}
          onChange={(preauthorized) => {
            onChange({ preauthorized });
          }}
        />
      </Switch.Group>
      <InputArea
        label="Tags"
        required={false}
        description={<TagsHint />}
        value={draft.tags}
        spellCheck={false}
        placeholder="tag:server, tag:prod"
        minRows={tagRows}
        onValueChange={(tags) => {
          onChange({ tags });
        }}
      />
      {groups === undefined ? null : (
        <MultiPicker
          label="Groups"
          description="Every machine registered with this key joins these groups."
          placeholder="Add groups…"
          items={groupItems(groups, { builtin: false })}
          value={draft.groupIds}
          onValueChange={(groupIds) => {
            onChange({ groupIds });
          }}
          empty="No group matches. Create one under Access controls."
        />
      )}
    </>
  );
}

function TagsHint(): ReactElement {
  return (
    <>
      Comma separated; <span className="font-mono text-[0.9em]">tag:</span> is added when missing. A
      tagged machine belongs to its tags instead of the user.
    </>
  );
}

function UserSelect({
  users,
  value,
  onChange,
}: {
  readonly users: readonly User[];
  readonly value: string;
  readonly onChange: (userId: string) => void;
}): ReactElement {
  const selected = users.find((user) => user.id === value);

  return (
    <Select
      className="w-full"
      label="User"
      value={value}
      renderValue={() => (selected === undefined ? value : userLabel(selected))}
      onValueChange={(userId: string | null) => {
        onChange(userId ?? "");
      }}
    >
      {users.map((user) => (
        <Select.Option key={user.id} value={user.id}>
          <span className="flex flex-col gap-0.5">
            <span className="font-medium text-kumo-default">{userLabel(user)}</span>
            <span className="text-sm text-kumo-subtle">
              {user.email === "" ? user.name : user.email}
            </span>
          </span>
        </Select.Option>
      ))}
    </Select>
  );
}

function OptionSwitch({
  label,
  description,
  checked,
  onChange,
}: {
  readonly label: string;
  readonly description: ReactNode;
  readonly checked: boolean;
  readonly onChange: (checked: boolean) => void;
}): ReactElement {
  return (
    <Switch
      checked={checked}
      onCheckedChange={onChange}
      label={
        <span className="flex flex-col gap-0.5">
          <span className="font-medium text-kumo-default">{label}</span>
          <span className="text-xs text-kumo-subtle">{description}</span>
        </span>
      }
    />
  );
}
