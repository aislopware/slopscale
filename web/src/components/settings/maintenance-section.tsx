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
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Server-side repairs an operator runs on purpose, each behind a confirmation. */
export function MaintenanceSection({ canRun }: { readonly canRun: boolean }): ReactElement {
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const backfill = api.useMutation("post", "/api/v1/node/backfillips", {
    onSuccess: async (data) => {
      await invalidate(queryClient, "/api/v1/node");
      setConfirming(false);
      toast.success(
        data.changes.length === 0
          ? "Every machine already has its addresses"
          : `Assigned addresses to ${data.changes.length === 1 ? "1 machine" : `${data.changes.length} machines`}`,
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
        description="Gives an IPv4 or IPv6 address to every machine missing one, for example after enabling a second address family in the server config."
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
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Backfill IP addresses?"
        description="Every machine without an address in a configured family gets one now. Existing addresses are kept."
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
