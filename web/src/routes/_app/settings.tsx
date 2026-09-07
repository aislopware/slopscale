import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { SignOutIcon } from "@phosphor-icons/react";
import { useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { invalidate, settingsQuery } from "~/api/queries.ts";
import type { Settings } from "~/api/queries.ts";
import type { UpdateSettingsRequestBody } from "~/api/schema.gen.ts";
import { can, displayName, roleLabel } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
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
        description="Tailnet-wide switches, the session this browser holds and the server it talks to."
      />
      <div className="flex max-w-3xl flex-col gap-6">
        <ApprovalSection settings={settings.data} canEdit={can(me, "feature_settings")} />
        <ConsoleSection me={me} />
        <ServerSection />
      </div>
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

/** Title and description on the left, the control on the right; hairlines between rows. */
function SettingRow({
  title,
  description,
  control,
}: {
  readonly title: string;
  readonly description: string;
  readonly control: ReactElement;
}): ReactElement {
  return (
    <SectionRow className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1">
        <span className="font-medium text-kumo-strong">{title}</span>
        <p className="max-w-prose text-kumo-subtle">{description}</p>
      </div>
      <span className="flex h-lh shrink-0 items-center">{control}</span>
    </SectionRow>
  );
}

function ApprovalSection({
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
    <Section title="Approval" description="Changes apply immediately." bodyClassName="p-0">
      {approvals.map((row) => (
        <SettingRow
          key={row.id}
          title={row.title}
          description={row.description}
          control={
            <Switch
              aria-label={row.title}
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
          }
        />
      ))}
    </Section>
  );
}

const kindLabels: Record<string, string> = {
  api_key: "API key",
  local: "Local socket",
  oauth: "OAuth token",
  session: "Browser session",
};

function consoleItems(me: Me): readonly Definition[] {
  const role = roleLabel(me);

  return [
    {
      label: "Signed in as",
      value: (
        <span className="flex items-center gap-1.5">
          {displayName(me)}
          <Badge variant="secondary">{kindLabels[me.kind] ?? me.kind}</Badge>
        </span>
      ),
    },
    {
      label: "Role",
      value:
        role === null ? (
          <span className="text-kumo-subtle">Not bound to a user</span>
        ) : (
          <Badge variant="info">{role}</Badge>
        ),
    },
    { label: "Scopes", value: <ScopeList me={me} /> },
  ];
}

function ConsoleSection({ me }: { readonly me: Me }): ReactElement {
  return (
    <Section
      title="Current session"
      description="Who this browser is signed in as. The console holds no privilege of its own."
      bodyClassName="p-0"
    >
      <DefinitionList items={consoleItems(me)} />
      <SettingRow
        title="Sign out"
        description="Ends this browser session. Machines and keys are unaffected."
        control={
          <Button
            variant="secondary"
            icon={SignOutIcon}
            onClick={() => {
              void signOut();
            }}
          >
            Sign out
          </Button>
        }
      />
    </Section>
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
    <span className="flex flex-wrap justify-end gap-1">
      {me.scopes.map((scope) => (
        <Badge key={scope} variant="secondary" className="font-mono text-[0.9em] font-normal">
          {scope}
        </Badge>
      ))}
    </span>
  );
}

function ServerSection(): ReactElement {
  const health = api.useQuery("get", "/api/v1/health");
  const reachable = health.data?.databaseConnectivity === true;

  return (
    <Section title="Server" description="What this console is talking to." bodyClassName="p-0">
      <DefinitionList
        items={[
          {
            label: "Database",
            value: (
              <Badge appearance="dot" variant={reachable ? "success" : "error"}>
                {reachable ? "Reachable" : "Unreachable"}
              </Badge>
            ),
          },
          {
            label: "API base",
            value: globalThis.location.origin,
            copy: globalThis.location.origin,
          },
        ]}
      />
    </Section>
  );
}
