import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Switch } from "@cloudflare/kumo/components/switch";
import type { ReactElement } from "react";

import type { Group, User } from "~/api/queries.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { expiryOptions } from "~/components/keys/expiration.ts";
import type { ExpiryChoice } from "~/components/keys/expiration.ts";
import { ChoiceChips } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { SettingsRow } from "~/components/ui/settings-card.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";
import { userHint, userLabel } from "~/lib/node.ts";

/**
 * The fields of a pre-auth key, shared by the Create pre-auth key page and the first step of Add
 * machine, which mints a single-use key with the same choices. Each takes its slice of the draft
 * and hands back a patch, so either form keeps its own state.
 */
export interface PreAuthKeyChoices {
  readonly userId: string;
  readonly expiry: ExpiryChoice;
  readonly ephemeral: boolean;
  readonly preauthorized: boolean;
  readonly tags: readonly string[];
  readonly groupIds: readonly string[];
}

type Patch = (patch: Partial<PreAuthKeyChoices>) => void;

/** The user a key belongs to, each option the user's name over the hint that tells two apart. */
export function UserSelect({
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
      {users.map((user) => {
        const hint = userHint(user);

        return (
          <Select.Option key={user.id} value={user.id}>
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">{userLabel(user)}</span>
              {hint === undefined ? null : <span className="text-sm text-kumo-subtle">{hint}</span>}
            </span>
          </Select.Option>
        );
      })}
    </Select>
  );
}

/** Whose the machines are: a user picked by name, or by id for a caller who may not list users. */
export function OwnerField({
  users,
  userId,
  onChange,
}: {
  /** The users to choose from, or undefined when the caller may not list them. */
  readonly users: readonly User[] | undefined;
  readonly userId: string;
  readonly onChange: Patch;
}): ReactElement {
  return users === undefined ? (
    <Input
      label="User id"
      description="Which user the key belongs to."
      value={userId}
      spellCheck={false}
      onChange={(event) => {
        onChange({ userId: event.target.value });
      }}
    />
  ) : (
    <UserSelect
      users={users}
      value={userId}
      onChange={(next) => {
        onChange({ userId: next });
      }}
    />
  );
}

/** How long the key registers machines for, as the dashboard's token expiration chips. */
export function ExpiryChoices({
  value,
  onChange,
}: {
  readonly value: ExpiryChoice;
  readonly onChange: Patch;
}): ReactElement {
  return (
    <ChoiceChips
      label="Key expiration"
      value={value}
      options={expiryOptions}
      onValueChange={(expiry) => {
        onChange({ expiry });
      }}
    />
  );
}

/**
 * What a registered machine is: one that goes away with its session, and one that skips approval.
 * Settings rows, so they go in a card that draws the edges (a `SettingsCard`, a flush
 * `FormSection`).
 */
export function MachineSettingsRows({
  ephemeral,
  preauthorized,
  onChange,
}: {
  readonly ephemeral: boolean;
  readonly preauthorized: boolean;
  readonly onChange: Patch;
}): ReactElement {
  return (
    <>
      <SettingsRow
        title="Ephemeral"
        description="The machine is deleted when it logs out or goes offline."
        control={
          <Switch
            aria-label="Ephemeral"
            checked={ephemeral}
            onCheckedChange={(next) => {
              onChange({ ephemeral: next });
            }}
          />
        }
      />
      <SettingsRow
        title="Pre-authorized"
        description="The machine skips approval."
        control={
          <Switch
            aria-label="Pre-authorized"
            checked={preauthorized}
            onCheckedChange={(next) => {
              onChange({ preauthorized: next });
            }}
          />
        }
      />
    </>
  );
}

/** The tags that make a machine tagged rather than the user's, and the groups it joins. */
export function TagsAndGroupsFields({
  tags,
  groupIds,
  groups,
  knownTags,
  onChange,
}: {
  readonly tags: readonly string[];
  readonly groupIds: readonly string[];
  /** The groups a registered machine may join, or undefined when the caller may not list them. */
  readonly groups: readonly Group[] | undefined;
  /** The tags the tailnet knows, offered as the operator types. */
  readonly knownTags: readonly string[];
  readonly onChange: Patch;
}): ReactElement {
  return (
    <>
      <TagField
        description="A tagged machine belongs to its tags, not the user."
        placeholder="tag:server"
        value={tags}
        suggestions={knownTags}
        onValueChange={(next) => {
          onChange({ tags: next });
        }}
      />
      {groups === undefined ? null : (
        <MultiPicker
          label="Groups"
          description="Every machine registered with this key joins these groups."
          placeholder="Add groups…"
          items={groupItems(groups, "membership")}
          value={groupIds}
          onValueChange={(next) => {
            onChange({ groupIds: next });
          }}
          empty="No group matches. Create one under Access controls."
        />
      )}
    </>
  );
}
