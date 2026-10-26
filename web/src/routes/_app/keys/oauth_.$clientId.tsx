import { LinkButton } from "@cloudflare/kumo/components/button";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { oauthClientsQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { emptyUsers } from "~/components/keys/api-columns.tsx";
import { useServerUrl } from "~/components/keys/federated-page.tsx";
import { OAuthClientEditor } from "~/components/keys/oauth-client-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/keys/oauth_/$clientId")({
  loader: async ({ context }) => {
    await context.queryClient.query(oauthClientsQuery);
  },
  component: OAuthClientPage,
});

function OAuthClientPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { clientId } = Route.useParams();
  const { oauthClients } = useSuspenseQuery(oauthClientsQuery).data;
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const serverUrl = useServerUrl(me);
  const client = oauthClients.find((candidate) => candidate.clientId === clientId);
  const name = client === undefined || client.description === "" ? clientId : client.description;

  useBreadcrumb(client === undefined ? "Client not found" : name);

  if (client === undefined) {
    return (
      <FormLayout>
        <PageHeader title="OAuth client not found" />
        <Section title="OAuth client" bodyClassName="p-0">
          <SectionEmpty
            title="This client does not exist"
            description="It may have been revoked since the link was made."
            contents={
              <LinkButton href="/keys/oauth" variant="secondary">
                Back to OAuth clients
              </LinkButton>
            }
          />
        </Section>
      </FormLayout>
    );
  }

  const federated = client.keyType === "federated";

  return (
    <FormLayout>
      <PageHeader
        title={name}
        kicker={federated ? "Federated identity" : "OAuth client"}
        description={
          federated
            ? "What a workload's OIDC token has to carry before the server trades it for an API token."
            : "The client exchanges its secret for short-lived v2 API tokens, limited to its scopes."
        }
      />
      <OAuthClientEditor
        key={client.clientId}
        client={client}
        me={me}
        canEdit={can(me, "oauth_keys")}
        users={users.data?.users ?? emptyUsers}
        serverUrl={serverUrl}
      />
    </FormLayout>
  );
}
