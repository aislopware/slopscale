import { LinkButton } from "@cloudflare/kumo/components/button";
import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { policyQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PolicyEditor } from "~/components/policy/policy-editor.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/policy")({
  loader: async ({ context }) => {
    await context.queryClient.query(policyQuery);
  },
  component: PolicyPage,
});

function PolicyPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const policy = useSuspenseQuery(policyQuery);

  return (
    <>
      <PageHeader
        title="Access controls"
        description="The tailnet policy in HuJSON: ACL grants, groups, tags, SSH rules and autogroups such as autogroup:shared."
        actions={
          <LinkButton
            href="https://headscale.net/stable/ref/policy/"
            external
            variant="secondary"
            icon={ArrowSquareOutIcon}
          >
            Policy reference
          </LinkButton>
        }
      />
      <PolicyEditor policy={policy.data} canEdit={can(me, "policy_file")} />
    </>
  );
}
