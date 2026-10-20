import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import { groupsQuery, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { expirationFor, expiryOptions } from "~/components/keys/expiration.ts";
import type { ExpiryChoice } from "~/components/keys/expiration.ts";
import { usePreAuthKeyMutations } from "~/components/keys/mutations.ts";
import { StepCard } from "~/components/machines/add-step-card.tsx";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";
import { userLabel } from "~/lib/node.ts";

interface Draft {
  readonly userId: string;
  readonly expiry: ExpiryChoice;
  readonly ephemeral: boolean;
  readonly preauthorized: boolean;
  readonly tags: readonly string[];
  readonly groupIds: readonly string[];
}

/**
 * Whose the machine is: the user its key belongs to, or its tags, plus how long the key lasts and
 * how the machine behaves once it is in. The key is for one machine; the Keys page makes reusable
 * ones.
 */
export function OwnerStep({
  me,
  onMinted,
}: {
  readonly me: Me;
  readonly onMinted: (key: string, summary: string) => void;
}): ReactElement {
  const mayListUsers = can(me, "users:read");
  const users = useQuery({ ...usersQuery, enabled: mayListUsers });
  const { create } = usePreAuthKeyMutations();
  const [draft, setDraft] = useState<Draft>({
    userId: me.user?.id ?? "",
    expiry: "1d",
    ephemeral: false,
    preauthorized: true,
    tags: [],
    groupIds: [],
  });
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
          reusable: false,
          ephemeral: draft.ephemeral,
          preauthorized: draft.preauthorized,
          expiration: expirationFor(draft.expiry),
          aclTags: [...draft.tags],
          groupIds: [...draft.groupIds],
        },
      },
      {
        onSuccess: (data) => {
          onMinted(data.preAuthKey.key, ownerSummary(draft, userList, userId));
        },
      },
    );
  }

  return (
    <form onSubmit={submit}>
      <StepCard
        title="Who it belongs to"
        description="The machine joins with a single-use key, so it belongs to the user you pick, or to its tags if you give it any."
        footer={
          <>
            <LinkButton href="/machines" variant="ghost">
              Cancel
            </LinkButton>
            <Button
              type="submit"
              variant="primary"
              loading={create.isPending}
              disabled={userId === ""}
            >
              Continue
            </Button>
          </>
        }
      >
        {mayListUsers ? (
          <Select
            className="w-full"
            label="User"
            value={userId}
            items={userList.map((user) => ({ value: user.id, label: userLabel(user) }))}
            onValueChange={(value: string | null) => {
              update({ userId: value ?? "" });
            }}
          />
        ) : (
          <Input
            label="User id"
            description="Which user the key belongs to."
            value={userId}
            spellCheck={false}
            onChange={(event) => {
              update({ userId: event.target.value });
            }}
          />
        )}
        <KeyOptions me={me} draft={draft} update={update} />
        {create.isError ? (
          <p role="alert" className="text-sm leading-snug text-kumo-danger">
            {errorMessage(create.error)}
          </p>
        ) : null}
      </StepCard>
    </form>
  );
}

/** What the key carries besides its user: tags, groups, how long it lasts and its options. */
function KeyOptions({
  me,
  draft,
  update,
}: {
  readonly me: Me;
  readonly draft: Draft;
  readonly update: (patch: Partial<Draft>) => void;
}): ReactElement {
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const knownTags = useKnownTags(me);

  return (
    <>
      <TagField
        description="A tagged machine belongs to its tags, not the user: use this for servers."
        placeholder="tag:server"
        value={draft.tags}
        suggestions={knownTags}
        onValueChange={(tags) => {
          update({ tags });
        }}
      />
      {groups.data === undefined ? null : (
        <MultiPicker
          label="Groups"
          description="The machine joins these groups as it registers."
          placeholder="Add groups…"
          items={groupItems(groups.data.groups, "membership")}
          value={draft.groupIds}
          onValueChange={(groupIds) => {
            update({ groupIds });
          }}
          empty="No group matches. Create one under Access controls."
        />
      )}
      <Select
        className="w-full"
        label="Key expires in"
        description="Only the key: the machine stays once it has joined."
        value={draft.expiry}
        items={expiryOptions}
        onValueChange={(expiry: ExpiryChoice | null) => {
          if (expiry !== null) {
            update({ expiry });
          }
        }}
      />
      <Switch.Group>
        <Switch.Legend>Options</Switch.Legend>
        <Switch
          checked={draft.preauthorized}
          onCheckedChange={(preauthorized) => {
            update({ preauthorized });
          }}
          label={<Option label="Pre-authorized" description="The machine skips approval." />}
        />
        <Switch
          checked={draft.ephemeral}
          onCheckedChange={(ephemeral) => {
            update({ ephemeral });
          }}
          label={
            <Option
              label="Ephemeral"
              description="The machine is deleted when it logs out or goes offline."
            />
          }
        />
      </Switch.Group>
    </>
  );
}

function Option({
  label,
  description,
}: {
  readonly label: string;
  readonly description: string;
}): ReactElement {
  return (
    <span className="flex flex-col gap-0.5">
      <span className="font-medium text-kumo-default">{label}</span>
      <span className="text-xs text-kumo-subtle">{description}</span>
    </span>
  );
}

/** "Key for Ada, tagged tag:server, expires in 1 day" — the first step folded into a line. */
function ownerSummary(draft: Draft, users: readonly User[], userId: string): string {
  const user = users.find((candidate) => candidate.id === userId);
  const person = user === undefined ? `user ${userId}` : userLabel(user);
  const owner = draft.tags.length > 0 ? draft.tags.join(", ") : person;
  const expiry = expiryOptions.find((option) => option.value === draft.expiry)?.label ?? "";

  return `Single-use key for ${owner}, expires in ${expiry}`;
}
