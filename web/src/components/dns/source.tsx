import { Banner } from "@cloudflare/kumo/components/banner";
import { ArrowCounterClockwiseIcon, FileTextIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Dns } from "~/api/queries.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { Callout } from "~/components/ui/callout.tsx";
import { Code } from "~/components/ui/code.tsx";
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
      <Callout
        icon={FileTextIcon}
        title="Settings come from the config file"
        description={
          <>
            Changes made here are stored in the database and replace the file&rsquo;s{" "}
            <Code>dns</Code> section until you reset. MagicDNS and the base domain always come from
            the file.
          </>
        }
      />
    );
  }

  return (
    <>
      <Callout
        icon={ArrowCounterClockwiseIcon}
        title="Settings were changed in the console"
        description={
          <>
            They replace the config file&rsquo;s <Code>dns</Code> section. Reset to go back to what
            the file says.
          </>
        }
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
  return (
    <span className="inline-flex items-center gap-1 text-xs text-kumo-subtle">
      <FileTextIcon size={12} aria-hidden />
      Config file
    </span>
  );
}
