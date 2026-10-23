import { Button } from "@cloudflare/kumo/components/button";
import {
  ArrowCounterClockwiseIcon,
  FileTextIcon,
  InfoIcon,
  PencilSimpleIcon,
} from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Dns } from "~/api/queries.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { Code } from "~/components/ui/code.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { HoverPopover } from "~/components/ui/hover-popover.tsx";

/**
 * Where a page's settings come from, the way the dashboard's DNS records page says "DNS Setup:
 * Full": a fact in `PageHeader`'s meta line, with the explanation one hover or focus away, so the
 * notice no longer takes a band of the page on every visit.
 */
export function SourceNote({
  overridden,
  fromFile,
  fromConsole,
}: {
  /** Whether the console's stored settings replace the file's. */
  readonly overridden: boolean;
  /** What editing here does while the file is in charge. */
  readonly fromFile: ReactNode;
  /** What the console's settings replace, and how to go back. */
  readonly fromConsole: ReactNode;
}): ReactElement {
  const Icon = overridden ? PencilSimpleIcon : FileTextIcon;

  return (
    <HoverPopover
      title={
        overridden ? "Settings were changed in the console" : "Settings come from the config file"
      }
      detail={overridden ? fromConsole : fromFile}
    >
      <span className="inline-flex items-center gap-1">
        <Icon aria-hidden className="size-3" />
        {overridden ? "Source: Console" : "Source: Config file"}
        <InfoIcon aria-hidden className="size-3 opacity-70" />
      </span>
    </HoverPopover>
  );
}

/**
 * The way back to the file once the console has replaced it: a secondary "Reset to config file" in
 * `PageHeader`'s actions, behind a confirmation. Nothing while the file is in charge.
 */
export function SourceActions({
  overridden,
  canEdit,
  reset,
}: {
  readonly overridden: boolean;
  readonly canEdit: boolean;
  readonly reset: {
    readonly title: string;
    readonly description: string;
    readonly pending: boolean;
    readonly error: string | undefined;
    readonly onConfirm: (done: () => void) => void;
  };
}): ReactElement | null {
  const [confirming, setConfirming] = useState(false);

  if (!overridden || !canEdit) {
    return null;
  }

  return (
    <>
      <Button
        variant="secondary"
        icon={ArrowCounterClockwiseIcon}
        onClick={() => {
          setConfirming(true);
        }}
      >
        Reset to config file
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={reset.title}
        description={reset.description}
        confirmLabel="Reset"
        loading={reset.pending}
        error={reset.error}
        onConfirm={() => {
          reset.onConfirm(() => {
            setConfirming(false);
          });
        }}
      />
    </>
  );
}

/** Where the DNS settings come from. */
export function DnsSourceNote({ dns }: { readonly dns: Dns }): ReactElement {
  return (
    <SourceNote
      overridden={dns.overridden}
      fromFile={
        <>
          Changes made here are stored in the database and replace the file&rsquo;s <Code>dns</Code>{" "}
          section until you reset. MagicDNS and the base domain always come from the file.
        </>
      }
      fromConsole={
        <>
          They replace the config file&rsquo;s <Code>dns</Code> section. Reset to go back to what
          the file says.
        </>
      }
    />
  );
}

/** The way back to the file once the DNS settings were edited here. */
export function DnsSourceActions({
  dns,
  canEdit,
  mutations,
}: {
  readonly dns: Dns;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement | null {
  return (
    <SourceActions
      overridden={dns.overridden}
      canEdit={canEdit}
      reset={{
        title: "Reset DNS settings?",
        description:
          "Nameservers, split DNS, search domains and extra records go back to the config file's values. Every machine gets the change at once.",
        pending: mutations.reset.isPending,
        error: mutations.reset.isError ? errorMessage(mutations.reset.error) : undefined,
        onConfirm: (done) => {
          mutations.reset.mutate({}, { onSuccess: done });
        },
      }}
    />
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
