import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import type { ReactElement } from "react";

import { accessRulesQuery, policyQuery, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PolicyFileTab } from "~/components/policy/policy-file-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";

export const Route = createFileRoute("/_app/policy/file")({
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(policyQuery),
      context.queryClient.query(accessRulesQuery),
    ]);
  },
  component: PolicyFilePage,
});

/** A user as the policy names it: the login name when it has an @ in it, else the email. */
function policyName(user: User): string {
  if (user.name.includes("@")) {
    return user.name;
  }

  return user.email.includes("@") ? user.email : "";
}

function PolicyFilePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const policy = useSuspenseQuery(policyQuery).data;
  const { rules, policyFileEnforces } = useSuspenseQuery(accessRulesQuery).data;
  // The names are only for completions, so the page does not wait for them: the editor gets the
  // list once it is there, and an operator who may not list users gets no user completions.
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const names = useMemo(
    () =>
      (users.data?.users ?? [])
        .map((user) => policyName(user))
        .filter((name) => name !== "")
        .toSorted((left, right) => left.localeCompare(right)),
    [users.data],
  );

  return (
    <>
      <PageHeader
        title="Policy file"
        description="The HuJSON policy for everything the rules do not say. Check validates a draft against the server without saving it."
      />
      <PolicyFileTab
        policy={policy}
        canEdit={can(me, "policy_file")}
        hasRules={rules.some((rule) => rule.enabled)}
        enforces={policyFileEnforces}
        users={names}
      />
    </>
  );
}
