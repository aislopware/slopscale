import { Banner } from "@cloudflare/kumo/components/banner";
import { ArrowCounterClockwiseIcon, FileTextIcon, WarningIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Derp } from "~/api/queries.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { Callout } from "~/components/ui/callout.tsx";
import { Code } from "~/components/ui/code.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";

/** Where the settings come from, and the way back to the file once they were edited here. */
export function SourceBanner({
  derp,
  canEdit,
  mutations,
}: {
  readonly derp: Derp;
  readonly canEdit: boolean;
  readonly mutations: DerpMutations;
}): ReactElement {
  const [confirming, setConfirming] = useState(false);

  if (!derp.overridden) {
    return (
      <Callout
        icon={FileTextIcon}
        title="Settings come from the config file"
        description={
          <>
            Changes made here are stored in the database and replace the file&rsquo;s{" "}
            <Code>derp</Code> section until you reset. Map files, the relay&rsquo;s key and{" "}
            <Code>automatically_add_embedded_derp_region</Code> always come from the file.
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
            They replace the config file&rsquo;s <Code>derp</Code> section. Reset to go back to what
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
        title="Reset relay settings?"
        description="Every relay setting goes back to the config file. The maps are fetched again and every machine gets the new map at once."
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

/**
 * The change was refused because the stored settings had moved on since this page read them, so
 * saving it would have overwritten what someone else changed.
 */
export function StaleSettingsBanner({
  mutations,
}: {
  readonly mutations: DerpMutations;
}): ReactElement | null {
  if (!mutations.stale) {
    return null;
  }

  return (
    <Callout
      tone="warning"
      title="The relay settings changed since this page loaded"
      description="Your change was not applied. Reload and try again."
      action={
        <Banner.Action
          variant="ghost"
          onClick={() => {
            mutations.reload();
          }}
        >
          Reload
        </Banner.Action>
      }
    />
  );
}

/** The last refetch failed: the map in use is the previous one. */
export function FetchErrorBanner({ derp }: { readonly derp: Derp }): ReactElement | null {
  if (derp.fetchError === "") {
    return null;
  }

  return (
    <Banner
      size="sm"
      variant="error"
      icon={<WarningIcon />}
      title="The last map fetch failed"
      description={`Machines keep the last map fetched. ${derp.fetchError}`}
    />
  );
}
