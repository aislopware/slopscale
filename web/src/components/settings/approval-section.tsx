import { Switch } from "@cloudflare/kumo/components/switch";
import type { ReactElement } from "react";

import type { Settings } from "~/api/queries.ts";
import type { UpdateSettingsRequestBody } from "~/api/schema.gen.ts";
import { useSettingsMutation } from "~/components/settings/mutations.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

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

export function ApprovalSection({
  settings,
  canEdit,
}: {
  readonly settings: Settings;
  readonly canEdit: boolean;
}): ReactElement {
  const update = useSettingsMutation();

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
