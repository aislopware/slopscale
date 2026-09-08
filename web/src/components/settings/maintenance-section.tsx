import { Button } from "@cloudflare/kumo/components/button";
import { ArrowsClockwiseIcon } from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Server-side repairs an operator runs on purpose, each behind a confirmation. */
export function MaintenanceSection({ canRun }: { readonly canRun: boolean }): ReactElement {
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [changes, setChanges] = useState<readonly string[] | null>(null);
  const backfill = api.useMutation("post", "/api/v1/node/backfillips", {
    onSuccess: async (data) => {
      await invalidate(queryClient, "/api/v1/node");
      setConfirming(false);
      setChanges(data.changes);
      toast.success(
        data.changes.length === 0
          ? "Nothing to change"
          : `Backfill made ${changeCount(data.changes)}`,
      );
    },
  });

  return (
    <Section
      title="Maintenance"
      description="Repairs that touch every machine. Each asks before it runs."
      bodyClassName="p-0"
    >
      <SettingRow
        title="Backfill IP addresses"
        description="Aligns every machine with the address families in the server config: a machine missing an IPv4 or IPv6 address gets one, and an address in a family no longer configured is removed."
        control={
          <Button
            variant="secondary"
            icon={ArrowsClockwiseIcon}
            disabled={!canRun}
            onClick={() => {
              setConfirming(true);
            }}
          >
            Backfill
          </Button>
        }
      />
      {changes === null ? null : <BackfillResult changes={changes} />}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Backfill IP addresses?"
        description="Every machine missing an address in a configured family gets one, and addresses in a family that is no longer configured are removed. Each change is listed afterwards."
        confirmLabel="Backfill"
        loading={backfill.isPending}
        error={backfill.isError ? errorMessage(backfill.error) : undefined}
        onConfirm={() => {
          backfill.mutate({ params: { query: { confirmed: true } } });
        }}
      />
    </Section>
  );
}

function changeCount(changes: readonly string[]): string {
  return changes.length === 1 ? "1 change" : `${changes.length} changes`;
}

/** What the last run did, one line per address as the server reported it. */
function BackfillResult({ changes }: { readonly changes: readonly string[] }): ReactElement {
  return (
    <SectionRow className="flex flex-col gap-2">
      <span className="text-kumo-subtle">
        {changes.length === 0
          ? "Last run: every machine already matched the configured address families."
          : `Last run: ${changeCount(changes)}.`}
      </span>
      {changes.length === 0 ? null : (
        <ul className="flex flex-col gap-1 font-mono text-xs text-kumo-default">
          {changes.map((change) => (
            <li key={change}>{change}</li>
          ))}
        </ul>
      )}
    </SectionRow>
  );
}
