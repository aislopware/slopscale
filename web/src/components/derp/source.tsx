import { Banner } from "@cloudflare/kumo/components/banner";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Derp } from "~/api/queries.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { SourceActions, SourceNote } from "~/components/dns/source.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { Code } from "~/components/ui/code.tsx";

/** Where the relay settings come from. */
export function DerpSourceNote({ derp }: { readonly derp: Derp }): ReactElement {
  return (
    <SourceNote
      overridden={derp.overridden}
      fromFile={
        <>
          Changes made here are stored in the database and replace the file&rsquo;s{" "}
          <Code>derp</Code> section until you reset. Map files, the relay&rsquo;s key and{" "}
          <Code>automatically_add_embedded_derp_region</Code> always come from the file.
        </>
      }
      fromConsole={
        <>
          They replace the config file&rsquo;s <Code>derp</Code> section. Reset to go back to what
          the file says.
        </>
      }
    />
  );
}

/** The way back to the file once the relay settings were edited here. */
export function DerpSourceActions({
  derp,
  canEdit,
  mutations,
}: {
  readonly derp: Derp;
  readonly canEdit: boolean;
  readonly mutations: DerpMutations;
}): ReactElement | null {
  return (
    <SourceActions
      overridden={derp.overridden}
      canEdit={canEdit}
      reset={{
        title: "Reset relay settings?",
        description:
          "Every relay setting goes back to the config file. The maps are fetched again and every machine gets the new map at once.",
        pending: mutations.reset.isPending,
        error: mutations.reset.isError ? errorMessage(mutations.reset.error) : undefined,
        onConfirm: (done) => {
          mutations.reset.mutate({}, { onSuccess: done });
        },
      }}
    />
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
    <Callout
      tone="error"
      icon={WarningIcon}
      title="The last map fetch failed"
      description={`Machines keep the last map fetched. ${derp.fetchError}`}
    />
  );
}
