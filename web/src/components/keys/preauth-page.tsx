import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { KeyIcon, RepeatIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import { groupsQuery, usersQuery } from "~/api/queries.ts";
import type { Group, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { RevealedSecret } from "~/components/keys/created-key.tsx";
import { expirationFor, expiryOptions } from "~/components/keys/expiration.ts";
import type { ExpiryChoice } from "~/components/keys/expiration.ts";
import {
  ChoiceChips,
  FormError,
  FormFooter,
  FormSection,
  SelectionCards,
} from "~/components/keys/form-page.tsx";
import type { SelectionCard } from "~/components/keys/form-page.tsx";
import { usePreAuthKeyMutations } from "~/components/keys/mutations.ts";
import { UserSelect } from "~/components/keys/preauth-dialogs.tsx";
import { JoinCommand } from "~/components/machines/join-command.tsx";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { SettingsRow } from "~/components/ui/settings-card.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";

const listPath = "/keys/pre-auth";

type Usage = "single" | "reusable";

const usageCards: readonly SelectionCard<Usage>[] = [
  {
    value: "single",
    title: "Single use",
    description: "Registers one machine, then stops working.",
    icon: KeyIcon,
  },
  {
    value: "reusable",
    title: "Reusable",
    description: "Registers any number of machines until it expires.",
    icon: RepeatIcon,
  },
];

interface Draft {
  readonly userId: string;
  readonly usage: Usage;
  readonly expiry: ExpiryChoice;
  readonly ephemeral: boolean;
  readonly preauthorized: boolean;
  readonly tags: readonly string[];
  readonly groupIds: readonly string[];
}

/**
 * Creating a pre-auth key on a page of its own, laid out like the dashboard's "Create API token":
 * one layered card per decision (who the machines belong to, how often the key works, what the
 * machines are, when it expires, what they join), then Cancel on the left and the action on the
 * right. Once the key exists the page turns into the one place it can be read.
 */
export function CreatePreAuthKeyPage({ me }: { readonly me: Me }): ReactElement {
  const [created, setCreated] = useState<string | null>(null);

  return created === null ? (
    <>
      <PageHeader
        title="Create pre-auth key"
        docs="ref/registration"
        description="A machine registers with the key instead of signing in, so it belongs to the user you pick, or to the tags you give it."
      />
      <PreAuthKeyForm me={me} onCreated={setCreated} />
    </>
  ) : (
    <>
      <PageHeader
        title="Pre-auth key created"
        docs="ref/registration"
        description="Copy the key before you leave this page. It is not shown again."
      />
      <div className="flex flex-col gap-4">
        <FormSection title="Your key">
          <RevealedSecret value={created} note="The key is shown only once. Copy it now." />
        </FormSection>
        <FormSection
          title="Register a machine"
          info="Run the command on the machine, or scan the code from a phone."
        >
          <JoinCommand authKey={created} />
        </FormSection>
        <FormFooter>
          <LinkButton href={listPath} variant="primary">
            Done
          </LinkButton>
        </FormFooter>
      </div>
    </>
  );
}

function PreAuthKeyForm({
  me,
  onCreated,
}: {
  readonly me: Me;
  readonly onCreated: (key: string) => void;
}): ReactElement {
  const mayListUsers = can(me, "users:read");
  const users = useQuery({ ...usersQuery, enabled: mayListUsers });
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const knownTags = useKnownTags(me);
  const { create } = usePreAuthKeyMutations();
  const [draft, setDraft] = useState<Draft>({
    userId: me.user?.id ?? "",
    usage: "single",
    expiry: "7d",
    ephemeral: false,
    preauthorized: true,
    tags: [],
    groupIds: [],
  });
  const userList = users.data?.users ?? [];
  const userId = draft.userId === "" ? (userList[0]?.id ?? "") : draft.userId;

  // An edit after a failed request clears the banner: the reader is fixing what it said.
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));

    if (create.isError) {
      create.reset();
    }
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    create.mutate(
      {
        body: {
          user: userId,
          reusable: draft.usage === "reusable",
          ephemeral: draft.ephemeral,
          preauthorized: draft.preauthorized,
          expiration: expirationFor(draft.expiry),
          aclTags: [...draft.tags],
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
      <FormError message={create.isError ? errorMessage(create.error) : undefined} />
      <OwnerSection
        users={mayListUsers ? userList : undefined}
        userId={mayListUsers ? userId : draft.userId}
        onChange={(next) => {
          update({ userId: next });
        }}
      />
      <FormSection title="Key usage" info="How many machines may register with the key.">
        <SelectionCards
          label="Key usage"
          value={draft.usage}
          cards={usageCards}
          onValueChange={(usage) => {
            update({ usage });
          }}
        />
      </FormSection>
      <MachineSettingsSection draft={draft} onChange={update} />
      <FormSection title="Key expiration" info="After this the key registers nothing more.">
        <ChoiceChips
          label="Key expiration"
          value={draft.expiry}
          options={expiryOptions}
          onValueChange={(expiry) => {
            update({ expiry });
          }}
        />
      </FormSection>
      <TagsSection
        draft={draft}
        groups={groups.data?.groups}
        knownTags={knownTags}
        onChange={update}
      />
      <FormFooter cancelTo={listPath}>
        <Button type="submit" variant="primary" loading={create.isPending} disabled={userId === ""}>
          Create key
        </Button>
      </FormFooter>
    </form>
  );
}

/** Whose the machines are: a user picked by name, or by id for a caller who may not list users. */
function OwnerSection({
  users,
  userId,
  onChange,
}: {
  /** The users to choose from, or undefined when the caller may not list them. */
  readonly users: readonly User[] | undefined;
  readonly userId: string;
  readonly onChange: (userId: string) => void;
}): ReactElement {
  return (
    <FormSection
      title="Owner"
      info="A machine registered with the key belongs to this user, unless the key tags it."
    >
      <div className="max-w-[450px]">
        {users === undefined ? (
          <Input
            label="User id"
            description="Which user the key belongs to."
            value={userId}
            spellCheck={false}
            onChange={(event) => {
              onChange(event.target.value);
            }}
          />
        ) : (
          <UserSelect users={users} value={userId} onChange={onChange} />
        )}
      </div>
    </FormSection>
  );
}

/** What a registered machine is: one that goes away with its session, and one that skips approval. */
function MachineSettingsSection({
  draft,
  onChange,
}: {
  readonly draft: Draft;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  return (
    <FormSection title="Machine settings" flush>
      <SettingsRow
        title="Ephemeral"
        description="The machine is deleted when it logs out or goes offline."
        control={
          <Switch
            aria-label="Ephemeral"
            checked={draft.ephemeral}
            onCheckedChange={(ephemeral) => {
              onChange({ ephemeral });
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
            checked={draft.preauthorized}
            onCheckedChange={(preauthorized) => {
              onChange({ preauthorized });
            }}
          />
        }
      />
    </FormSection>
  );
}

/** The tags that make a machine tagged rather than the user's, and the groups it joins. */
function TagsSection({
  draft,
  groups,
  knownTags,
  onChange,
}: {
  readonly draft: Draft;
  /** The groups a registered machine may join, or undefined when the caller may not list them. */
  readonly groups: readonly Group[] | undefined;
  readonly knownTags: readonly string[];
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  return (
    <FormSection
      title="Tags and groups"
      info="A tagged machine belongs to its tags, not the user. Every machine registered with the key joins the groups."
    >
      <TagField
        description="A tagged machine belongs to its tags, not the user."
        placeholder="tag:server"
        value={draft.tags}
        suggestions={knownTags}
        onValueChange={(tags) => {
          onChange({ tags });
        }}
      />
      {groups === undefined ? null : (
        <MultiPicker
          label="Groups"
          description="Every machine registered with this key joins these groups."
          placeholder="Add groups…"
          items={groupItems(groups, "membership")}
          value={draft.groupIds}
          onValueChange={(groupIds) => {
            onChange({ groupIds });
          }}
          empty="No group matches. Create one under Access controls."
        />
      )}
    </FormSection>
  );
}
