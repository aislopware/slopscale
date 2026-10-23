import { LinkButton } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { EyeIcon, PlusIcon, UserIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { managesAllKeys, userOptions } from "~/components/keys/api-dialogs.tsx";
import { RevealedSecret } from "~/components/keys/created-key.tsx";
import { expirationFor, expiryOptions } from "~/components/keys/expiration.ts";
import type { ExpiryChoice } from "~/components/keys/expiration.ts";
import { useApiKeyMutations } from "~/components/keys/mutations.ts";
import { scopeItems } from "~/components/keys/scopes.ts";
import {
  ChoiceChips,
  FormBar,
  FormLayout,
  FormPage,
  FormSection,
  SelectionCards,
  nameFieldClass,
} from "~/components/ui/form-page.tsx";
import type { SelectionCard } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

const listPath = "/keys/api";

/** Which permissions the key carries: its owner's, read-only, or a set picked by hand. */
type Permissions = "owner" | "read" | "custom";

const readScope = "all:read";

function permissionCards(me: Me): SelectionCard<Permissions>[] {
  const mayRead = scopeItems(me).some((item) => item.value === readScope);

  return [
    {
      value: "owner",
      title: "Owner's permissions",
      description: "Everything the key's owner may do, following their role.",
      icon: UserIcon,
    },
    ...(mayRead
      ? [
          {
            value: "read" as const,
            title: "Read everything",
            description: "Reads every resource and changes nothing.",
            icon: EyeIcon,
          },
        ]
      : []),
    {
      value: "custom",
      title: "Pick scopes",
      badge: "Custom",
      description: "Limits the key to the operations you choose.",
      icon: PlusIcon,
    },
  ];
}

function scopesFor(permissions: Permissions, picked: readonly string[]): string[] {
  if (permissions === "read") {
    return [readScope];
  }

  return permissions === "custom" ? [...picked] : [];
}

/**
 * Creating an API key on a page of its own, the dashboard's "Create API token": the key's name, its
 * permissions as template cards (the owner's, read-only, or picked scopes), who it acts as, and its
 * expiration as chips, then the save bar with Cancel and the action. The secret is shown on the
 * same page once it exists.
 */
export function CreateApiKeyPage({ me }: { readonly me: Me }): ReactElement {
  const [created, setCreated] = useState<string | null>(null);

  return created === null ? (
    <FormLayout>
      <PageHeader
        title="Create API key"
        docs="ref/api"
        description="The key authenticates calls to the v1 API with its owner's permissions, or with the scopes you pick."
      />
      <ApiKeyForm me={me} onCreated={setCreated} />
    </FormLayout>
  ) : (
    <FormLayout>
      <PageHeader
        title="API key created"
        docs="ref/api"
        description="Copy the key before you leave this page. It is not shown again."
      />
      <FormSection title="Your key">
        <RevealedSecret value={created} note="The key is shown only once. Copy it now." />
      </FormSection>
      <FormBar>
        <LinkButton href={listPath} variant="primary">
          Done
        </LinkButton>
      </FormBar>
    </FormLayout>
  );
}

function ApiKeyForm({
  me,
  onCreated,
}: {
  readonly me: Me;
  readonly onCreated: (key: string) => void;
}): ReactElement {
  const forOthers = managesAllKeys(me) && can(me, "users:read");
  const users = useQuery({ ...usersQuery, enabled: forOthers });
  const { create } = useApiKeyMutations();
  const [description, setDescription] = useState("");
  const [permissions, setPermissions] = useState<Permissions>("owner");
  const [picked, setPicked] = useState<readonly string[]>([]);
  const [userId, setUserId] = useState(me.user?.id ?? "");
  const [expiry, setExpiry] = useState<ExpiryChoice>("90d");
  const [tried, setTried] = useState(false);
  const scopeIssue =
    permissions === "custom" && picked.length === 0
      ? "Pick at least one scope, or give the key its owner's permissions."
      : undefined;

  // Any edit after a failed request clears the banner: the reader is fixing what it said.
  const edited = (): void => {
    if (create.isError) {
      create.reset();
    }
  };

  function submit(): boolean {
    setTried(true);

    if (scopeIssue !== undefined) {
      return false;
    }

    create.mutate(
      {
        body: {
          expiration: expirationFor(expiry),
          description: description.trim(),
          scopes: scopesFor(permissions, picked),
          ...(userId === "" ? {} : { userId }),
        },
      },
      {
        onSuccess: (data) => {
          onCreated(data.apiKey);
        },
      },
    );

    return true;
  }

  return (
    <FormPage
      noun="API key"
      creating
      canEdit
      error={create.isError ? errorMessage(create.error) : undefined}
      errorTitle="The key was not created"
      pending={create.isPending}
      dirty={description !== "" || permissions !== "owner" || picked.length > 0 || expiry !== "90d"}
      onSubmit={submit}
      cancelTo={listPath}
    >
      <FormSection title="Key description" info="What the key is for, so it can be told apart.">
        <Input
          aria-label="Key description"
          className={nameFieldClass}
          value={description}
          placeholder="CI deploy, monitoring…"
          onChange={(event) => {
            setDescription(event.target.value);
            edited();
          }}
        />
      </FormSection>
      <PermissionsSection
        me={me}
        permissions={permissions}
        picked={picked}
        error={tried ? scopeIssue : undefined}
        onPermissionsChange={(value) => {
          setPermissions(value);
          edited();
        }}
        onPickedChange={(value) => {
          setPicked(value);
          edited();
        }}
      />
      {forOthers ? (
        <FormSection title="Owner" info="The key acts with this user's role.">
          <Select
            className="w-full max-w-[450px]"
            label="For user"
            value={userId}
            items={userOptions(users.data?.users)}
            onValueChange={(value: string | null) => {
              setUserId(value ?? "");
              edited();
            }}
          />
        </FormSection>
      ) : null}
      <FormSection title="Key expiration" info="After this the key is refused.">
        <ChoiceChips
          label="Key expiration"
          value={expiry}
          options={expiryOptions}
          onValueChange={(value) => {
            setExpiry(value);
            edited();
          }}
        />
      </FormSection>
    </FormPage>
  );
}

/** The permission template cards, and the scope picker under them once "Pick scopes" is chosen. */
function PermissionsSection({
  me,
  permissions,
  picked,
  error,
  onPermissionsChange,
  onPickedChange,
}: {
  readonly me: Me;
  readonly permissions: Permissions;
  readonly picked: readonly string[];
  /** Shown under the picker after a submit that it stopped. */
  readonly error: string | undefined;
  readonly onPermissionsChange: (value: Permissions) => void;
  readonly onPickedChange: (value: string[]) => void;
}): ReactElement {
  return (
    <FormSection
      title="Permissions"
      info="A key never does more than its owner may. Scopes narrow it further."
    >
      <SelectionCards
        label="Permissions"
        value={permissions}
        cards={permissionCards(me)}
        onValueChange={onPermissionsChange}
      />
      {permissions === "custom" ? (
        <div className="flex flex-col gap-1.5">
          <MultiPicker
            label="Scopes"
            description="The operations the key may call. You can only grant scopes you hold."
            placeholder="Pick scopes"
            items={scopeItems(me)}
            value={picked}
            onValueChange={onPickedChange}
            empty="No scope matches."
          />
          {error === undefined ? null : (
            <p role="alert" className="text-sm leading-snug text-kumo-danger">
              {error}
            </p>
          )}
        </div>
      ) : null}
    </FormSection>
  );
}
