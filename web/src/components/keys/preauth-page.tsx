import { LinkButton } from "@cloudflare/kumo/components/button";
import { KeyIcon, RepeatIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { groupsQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { RevealedSecret } from "~/components/keys/created-key.tsx";
import { expirationFor } from "~/components/keys/expiration.ts";
import { usePreAuthKeyMutations } from "~/components/keys/mutations.ts";
import {
  ExpiryChoices,
  MachineSettingsRows,
  OwnerField,
  TagsAndGroupsFields,
} from "~/components/keys/preauth-fields.tsx";
import type { PreAuthKeyChoices } from "~/components/keys/preauth-fields.tsx";
import { JoinCommand } from "~/components/machines/join-command.tsx";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import {
  FormBar,
  FormLayout,
  FormPage,
  FormSection,
  SelectionCards,
} from "~/components/ui/form-page.tsx";
import type { SelectionCard } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

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

interface Draft extends PreAuthKeyChoices {
  readonly usage: Usage;
}

/**
 * Creating a pre-auth key on a page of its own, laid out like the dashboard's "Create API token":
 * one layered card per decision (who the machines belong to, how often the key works, what the
 * machines are, when it expires, what they join), then the save bar with Cancel and the action.
 * Once the key exists the page turns into the one place it can be read.
 */
export function CreatePreAuthKeyPage({ me }: { readonly me: Me }): ReactElement {
  const [created, setCreated] = useState<string | null>(null);

  return created === null ? (
    <FormLayout>
      <PageHeader
        title="Create pre-auth key"
        description="A machine registers with the key instead of signing in, so it belongs to the user you pick, or to the tags you give it."
      />
      <PreAuthKeyForm me={me} onCreated={setCreated} />
    </FormLayout>
  ) : (
    <FormLayout>
      <PageHeader
        title="Pre-auth key created"
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
      </div>
      <FormBar>
        <LinkButton href={listPath} variant="primary">
          Done
        </LinkButton>
      </FormBar>
    </FormLayout>
  );
}

function initialDraft(me: Me): Draft {
  return {
    userId: me.user?.id ?? "",
    usage: "single",
    expiry: "7d",
    ephemeral: false,
    preauthorized: true,
    tags: [],
    groupIds: [],
  };
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
  const initial = initialDraft(me);
  const [draft, setDraft] = useState<Draft>(initial);
  const userList = users.data?.users ?? [];
  const userId = draft.userId === "" ? (userList[0]?.id ?? "") : draft.userId;

  // An edit after a failed request clears the banner: the reader is fixing what it said.
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));

    if (create.isError) {
      create.reset();
    }
  };

  function submit(): boolean {
    if (userId === "") {
      return false;
    }

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

    return true;
  }

  return (
    <FormPage
      noun="key"
      creating
      canEdit
      error={create.isError ? errorMessage(create.error) : undefined}
      errorTitle="The key was not created"
      pending={create.isPending}
      dirty={JSON.stringify(draft) !== JSON.stringify(initial)}
      onSubmit={submit}
      cancelTo={listPath}
    >
      <FormSection
        title="Owner"
        info="A machine registered with the key belongs to this user, unless the key tags it."
      >
        <div className="max-w-[450px]">
          <OwnerField
            users={mayListUsers ? userList : undefined}
            userId={mayListUsers ? userId : draft.userId}
            onChange={update}
          />
        </div>
      </FormSection>
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
      <FormSection title="Machine settings" flush>
        <MachineSettingsRows
          ephemeral={draft.ephemeral}
          preauthorized={draft.preauthorized}
          onChange={update}
        />
      </FormSection>
      <FormSection title="Key expiration" info="After this the key registers nothing more.">
        <ExpiryChoices value={draft.expiry} onChange={update} />
      </FormSection>
      <FormSection
        title="Tags and groups"
        info="A tagged machine belongs to its tags, not the user. Every machine registered with the key joins the groups."
      >
        <TagsAndGroupsFields
          tags={draft.tags}
          groupIds={draft.groupIds}
          groups={groups.data?.groups}
          knownTags={knownTags}
          onChange={update}
        />
      </FormSection>
    </FormPage>
  );
}
