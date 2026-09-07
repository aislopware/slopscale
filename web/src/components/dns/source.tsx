import { Badge } from "@cloudflare/kumo/components/badge";
import { Banner } from "@cloudflare/kumo/components/banner";
import { ArrowCounterClockwiseIcon, FileTextIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Dns } from "~/api/queries.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";

/** Where the settings come from, and the way back to the file once they were edited here. */
export function SourceBanner({
  dns,
  canEdit,
  mutations,
}: {
  readonly dns: Dns;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement {
  const [confirming, setConfirming] = useState(false);

  if (!dns.overridden) {
    return (
      <Banner
        size="sm"
        variant="default"
        icon={<FileTextIcon />}
        title="Settings come from the config file"
        description="Changes made here are stored in the database and replace the file's dns section until you reset. MagicDNS and the base domain always come from the file."
      />
    );
  }

  return (
    <>
      <Banner
        size="sm"
        variant="default"
        icon={<ArrowCounterClockwiseIcon />}
        title="Settings were changed in the console"
        description="They replace the config file's dns section. Reset to go back to what the file says."
        {...(canEdit
          ? {
              action: (
                <Banner.Action
                  variant="ghost"
                  onClick={() => {
                    setConfirming(true);
                  }}
                >
                  Reset to config file
                </Banner.Action>
              ),
            }
          : {})}
      />
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Reset DNS settings?"
        description="Nameservers, split DNS, search domains and extra records go back to the config file's values. Every machine gets the change at once."
        confirmLabel="Reset"
        loading={mutations.reset.isPending}
        error={mutations.reset.isError ? errorMessage(mutations.reset.error) : undefined}
        onConfirm={() => {
          mutations.reset.mutate(
            {},
            {
              onSuccess: () => {
                setConfirming(false);
              },
            },
          );
        }}
      />
    </>
  );
}

/** Marks a value the config file owns. */
export function FromFileBadge(): ReactElement {
  return <Badge variant="secondary">Config file</Badge>;
}
