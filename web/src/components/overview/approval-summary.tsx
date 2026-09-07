import { Badge } from "@cloudflare/kumo/components/badge";
import { Link } from "@cloudflare/kumo/components/link";
import type { ReactElement } from "react";

import type { Settings } from "~/api/queries.ts";
import { Card, CardBody, CardHeader, CardTitle } from "~/components/ui/card.tsx";

function Line({ label, on }: { readonly label: string; readonly on: boolean }): ReactElement {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-kumo-subtle">{label}</dt>
      <dd>
        <Badge appearance="dot" variant={on ? "success" : "neutral"}>
          {on ? "On" : "Off"}
        </Badge>
      </dd>
    </div>
  );
}

export interface ApprovalSummaryProps {
  readonly settings: Settings;
}

export function ApprovalSummary({ settings }: ApprovalSummaryProps): ReactElement {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Approval settings</CardTitle>
      </CardHeader>
      <CardBody className="flex flex-col gap-4">
        <dl className="flex flex-col gap-2">
          <Line label="Device approval" on={settings.devicesApprovalOn} />
          <Line label="User approval" on={settings.usersApprovalOn} />
        </dl>
        <Link href="/settings" variant="plain">
          Change settings
        </Link>
      </CardBody>
    </Card>
  );
}
