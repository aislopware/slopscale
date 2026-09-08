import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";
import { fallback, object, optional, string } from "valibot";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { DialogError } from "~/components/ui/dialog.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { toast } from "~/components/ui/toast.ts";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { userLabel } from "~/lib/node.ts";

const optionalText = optional(fallback(string(), ""), "");
const searchSchema = object({ key: optionalText });

export const Route = createFileRoute("/_app/machines/register")({
  validateSearch: searchSchema,
  // Registering a machine is a write on machines; a reader has nothing to do here.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "devices:core")) {
      throw redirect({ to: "/machines" });
    }
  },
  loader: async ({ context }) => {
    if (can(context.me, "users:read")) {
      await context.queryClient.query(usersQuery);
    }
  },
  component: RegisterPage,
});

/**
 * Completes an interactive login: a machine that signed in without a pre-auth key or an identity
 * provider shows a registration key on its login page, and this form binds it to a user. The login
 * page links here with the key filled in.
 */
function RegisterPage(): ReactElement {
  useBreadcrumb("Register");
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const [key, setKey] = useState(search.key);
  const [user, setUser] = useState(me.user?.name ?? "");
  const register = api.useMutation("post", "/api/v1/node/register", {
    onSuccess: async (data) => {
      await invalidate(queryClient, "/api/v1/node");
      toast.success(`${data.node.givenName} registered`);
      await navigate({ to: "/machines/$nodeId", params: { nodeId: data.node.id } });
    },
  });

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    register.mutate({ params: { query: { key: key.trim(), user: user.trim() } } });
  }

  return (
    <>
      <PageHeader
        title="Register a machine"
        description="A machine that signed in without a key or an identity provider is waiting with a registration key. Bind it to a user to let it in."
      />
      <LayerCard className="max-w-xl p-5">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Input
            label="Registration key"
            description="From the machine's login page, or the tailscale up output."
            value={key}
            required
            spellCheck={false}
            className="font-mono"
            onChange={(event) => {
              setKey(event.target.value);
            }}
          />
          <UserField users={users.data?.users} value={user} onChange={setUser} />
          <DialogError message={register.isError ? errorMessage(register.error) : undefined} />
          <div className="flex justify-end">
            <Button
              type="submit"
              variant="primary"
              disabled={key.trim() === "" || user.trim() === ""}
              loading={register.isPending}
            >
              Register machine
            </Button>
          </div>
        </form>
      </LayerCard>
    </>
  );
}

/** A pick from the user list when the caller may read it; otherwise the name typed in. */
function UserField({
  users,
  value,
  onChange,
}: {
  readonly users: readonly User[] | undefined;
  readonly value: string;
  readonly onChange: (name: string) => void;
}): ReactElement {
  if (users === undefined) {
    return (
      <Input
        label="User"
        description="The user the machine belongs to, by name."
        value={value}
        required
        onChange={(event) => {
          onChange(event.target.value);
        }}
      />
    );
  }

  return (
    <Select
      className="w-full"
      label="User"
      description="The machine belongs to this user."
      value={value}
      items={users.map((user) => ({ value: user.name, label: userLabel(user) }))}
      onValueChange={(name: string | null) => {
        onChange(name ?? "");
      }}
    />
  );
}
