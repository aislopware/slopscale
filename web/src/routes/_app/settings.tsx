import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { SignOutIcon } from "@phosphor-icons/react";
import { useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import { api } from "~/api/client.ts";
import { invalidate, settingsQuery } from "~/api/queries.ts";
import type { Settings } from "~/api/queries.ts";
import type { UpdateSettingsRequestBody } from "~/api/schema.gen.ts";
import { can, displayName, roleLabel } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import { Card, CardHeader, CardTitle } from "~/components/ui/card.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { toast } from "~/components/ui/toast.ts";

export const Route = createFileRoute("/_app/settings")({
  loader: async ({ context }) => {
    await context.queryClient.query(settingsQuery);
  },
  component: SettingsPage,
});

function SettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const settings = useSuspenseQuery(settingsQuery);

  return (
    <>
      <PageHeader
        title="Settings"
        description="Tailnet-wide switches. Changes apply immediately."
      />
      <ApprovalCard settings={settings.data} canEdit={can(me, "feature_settings")} />
      <ConsoleCard me={me} />
      <ServerCard />
    </>
  );
}

interface ApprovalSwitch {
  readonly id: string;
  readonly title: string;
  readonly description: string;
  readonly read: (settings: Settings) => boolean;
  readonly write: (on: boolean) => UpdateSettingsRequestBody;
}

const approvals: readonly ApprovalSwitch[] = [
  {
    id: "devices",
    title: "Device approval",
    description:
      "New machines must be approved by an administrator before they can reach the tailnet. Turning this off approves every machine that is waiting.",
    read: (settings) => settings.devicesApprovalOn,
    write: (on) => ({ devicesApprovalOn: on }),
  },
  {
    id: "users",
    title: "User approval",
    description:
      "New users must be approved before their machines are accepted. Turning this off approves every user that is waiting.",
    read: (settings) => settings.usersApprovalOn,
    write: (on) => ({ usersApprovalOn: on }),
  },
];

function ApprovalCard({
  settings,
  canEdit,
}: {
  readonly settings: Settings;
  readonly canEdit: boolean;
}): ReactElement {
  const queryClient = useQueryClient();
  const update = api.useMutation("post", "/api/v1/settings", {
    onSuccess: async () => {
      await invalidate(queryClient, "/api/v1/settings", "/api/v1/node", "/api/v1/user");
    },
    onError: (error) => {
      toast.error("Could not change the setting", error);
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Approval</CardTitle>
      </CardHeader>
      <div className="flex flex-col divide-y divide-kumo-line">
        {approvals.map((row) => (
          <div key={row.id} className="flex flex-col gap-1.5 px-5 py-4">
            <Switch
              label={row.title}
              checked={row.read(settings)}
              disabled={!canEdit || update.isPending}
              transitioning={update.isPending}
              onCheckedChange={(on) => {
                update.mutate(
                  { body: row.write(on) },
                  {
                    onSuccess: () => {
                      toast.success(`${row.title} ${on ? "on" : "off"}`);
                    },
                  },
                );
              }}
            />
            <p className="max-w-prose text-kumo-subtle">{row.description}</p>
          </div>
        ))}
      </div>
    </Card>
  );
}

const kindLabels: Record<string, string> = {
  api_key: "API key",
  local: "Local socket",
  oauth: "OAuth token",
};

function Row({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-center justify-between gap-4 px-5 py-3">
      <dt className="text-kumo-subtle">{label}</dt>
      <dd className="flex flex-wrap items-center justify-end gap-1.5 text-kumo-default">
        {children}
      </dd>
    </div>
  );
}

function ConsoleCard({ me }: { readonly me: Me }): ReactElement {
  const role = roleLabel(me);

  return (
    <Card>
      <CardHeader>
        <CardTitle>This console</CardTitle>
      </CardHeader>
      <dl className="flex flex-col divide-y divide-kumo-line">
        <Row label="Signed in as">
          {displayName(me)}
          {me.user === undefined ? null : (
            <Badge variant="neutral">{kindLabels[me.kind] ?? me.kind}</Badge>
          )}
        </Row>
        <Row label="Role">
          {role === null ? (
            <span className="text-kumo-subtle">Not bound to a user</span>
          ) : (
            <Badge variant="info">{role}</Badge>
          )}
        </Row>
        <Row label="Scopes">
          <ScopeList me={me} />
        </Row>
      </dl>
      <div className="border-t border-kumo-line px-5 py-4">
        <Button
          variant="secondary"
          icon={SignOutIcon}
          onClick={() => {
            void signOut();
          }}
        >
          Sign out
        </Button>
      </div>
    </Card>
  );
}

function ScopeList({ me }: { readonly me: Me }): ReactElement {
  if (me.allAccess) {
    return <Badge variant="success">All access</Badge>;
  }

  if (me.scopes.length === 0) {
    return <span className="text-kumo-subtle">None</span>;
  }

  return (
    <>
      {me.scopes.map((scope) => (
        <Badge key={scope} variant="neutral" className="font-mono text-[0.9em] font-normal">
          {scope}
        </Badge>
      ))}
    </>
  );
}

function ServerCard(): ReactElement {
  const health = api.useQuery("get", "/api/v1/health");

  return (
    <Card>
      <CardHeader>
        <CardTitle>Server</CardTitle>
      </CardHeader>
      <dl className="flex flex-col divide-y divide-kumo-line">
        <Row label="Database">
          {health.data?.databaseConnectivity === true ? (
            <Badge appearance="dot" variant="success">
              Reachable
            </Badge>
          ) : (
            <Badge appearance="dot" variant="error">
              Unreachable
            </Badge>
          )}
        </Row>
        <Row label="API base">
          <span className="font-mono text-[0.9em]">{globalThis.location.origin}</span>
        </Row>
      </dl>
    </Card>
  );
}
