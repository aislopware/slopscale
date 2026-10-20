import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import { groupsQuery, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { expirationFor, expiryOptions } from "~/components/keys/expiration.ts";
import { usePreAuthKeyMutations } from "~/components/keys/mutations.ts";
import {
  ExpiryChoices,
  MachineSettingsRows,
  OwnerField,
  TagsAndGroupsFields,
} from "~/components/keys/preauth-fields.tsx";
import type { PreAuthKeyChoices } from "~/components/keys/preauth-fields.tsx";
import { StepCard } from "~/components/machines/add-step-card.tsx";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { SettingsCard } from "~/components/ui/settings-card.tsx";
import { userLabel } from "~/lib/node.ts";

type Draft = PreAuthKeyChoices;

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
            <LinkButton href="/machines" variant="ghost" className="mr-auto">
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
        <OwnerField
          users={mayListUsers ? userList : undefined}
          userId={mayListUsers ? userId : draft.userId}
          onChange={update}
        />
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
      <TagsAndGroupsFields
        tags={draft.tags}
        groupIds={draft.groupIds}
        groups={groups.data?.groups}
        knownTags={knownTags}
        onChange={update}
      />
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 font-medium text-kumo-default">Key expires in</legend>
        <ExpiryChoices value={draft.expiry} onChange={update} />
        <p className="text-sm text-kumo-subtle">
          Only the key: the machine stays once it has joined.
        </p>
      </fieldset>
      <SettingsCard>
        <MachineSettingsRows
          ephemeral={draft.ephemeral}
          preauthorized={draft.preauthorized}
          onChange={update}
        />
      </SettingsCard>
    </>
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
