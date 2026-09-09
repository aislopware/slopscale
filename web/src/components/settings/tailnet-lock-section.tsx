import { Button } from "@cloudflare/kumo/components/button";
import { Link } from "@tanstack/react-router";
import { Fragment, useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import type { TailnetLock } from "~/api/queries.ts";
import { useDisableTailnetLockMutation } from "~/components/settings/mutations.ts";
import { Code } from "~/components/ui/code.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";

const noSecretReason =
  "No support disablement secret was recorded when the lock was enabled. Run tailscale lock disable <secret> on a machine.";

function disableReason(canEdit: boolean, lock: TailnetLock): string | undefined {
  if (!canEdit) {
    return "Your credentials cannot change settings";
  }

  if (!lock.supportDisablementAvailable) {
    return noSecretReason;
  }

  return undefined;
}

function trustedKeysValue(lock: TailnetLock): ReactNode {
  if (!lock.enabled || lock.keys.length === 0) {
    return <span className="text-kumo-subtle">None</span>;
  }

  return (
    <div className="flex flex-col items-end gap-1">
      {lock.keys.map((key) => (
        <span key={key.id} className="flex items-center gap-2">
          <Code>{key.public}</Code>
          <span className="text-kumo-subtle">votes {key.votes}</span>
        </span>
      ))}
    </div>
  );
}

function signedMachinesValue(lock: TailnetLock): ReactNode {
  if (lock.unsignedNodeIds.length === 0) {
    return String(lock.signedNodeIds.length);
  }

  const unsignedLabel = `${lock.unsignedNodeIds.length} waiting for a signature`;

  return (
    <span className="flex flex-wrap items-center justify-end gap-2">
      <span>{lock.signedNodeIds.length}</span>
      <span className="text-kumo-subtle">
        ({unsignedLabel}:{" "}
        {lock.unsignedNodeIds.map((id, index) => (
          <Fragment key={id}>
            {index > 0 ? ", " : null}
            <Link
              to="/machines/$nodeId"
              params={{ nodeId: id }}
              className="text-kumo-link underline decoration-kumo-line underline-offset-2 hover:decoration-current"
            >
              {id}
            </Link>
          </Fragment>
        ))}
        )
      </span>
    </span>
  );
}

function buildDefinitions(lock: TailnetLock): readonly Definition[] {
  return [
    {
      label: "State",
      value: (
        <Status tone={lock.enabled ? "success" : "neutral"}>{lock.enabled ? "On" : "Off"}</Status>
      ),
    },
    ...(lock.enabled
      ? [
          {
            label: "Head",
            value: <CopyText value={lock.head} label="Copy head hash" />,
          },
        ]
      : []),
    {
      label: "Trusted keys",
      value: trustedKeysValue(lock),
    },
    {
      label: "Signed machines",
      value: signedMachinesValue(lock),
    },
    ...(lock.enabledAt !== undefined && lock.enabledAt !== null && lock.enabledAt !== ""
      ? [
          {
            label: "Enabled at",
            value: <RelativeTime value={lock.enabledAt} />,
          },
        ]
      : []),
    ...(lock.disabledAt !== undefined && lock.disabledAt !== null && lock.disabledAt !== ""
      ? [
          {
            label: "Disabled at",
            value: <RelativeTime value={lock.disabledAt} />,
          },
        ]
      : []),
  ];
}

/** Tailnet lock state, signing keys and machine signatures. */
export function TailnetLockSection({
  lock,
  canEdit,
}: {
  readonly lock: TailnetLock;
  readonly canEdit: boolean;
}): ReactElement {
  const [confirming, setConfirming] = useState(false);
  const disable = useDisableTailnetLockMutation();
  const reason = disableReason(canEdit, lock);

  const actions = lock.enabled ? (
    <DisabledReason reason={reason}>
      <Button
        variant="destructive"
        disabled={reason !== undefined || disable.isPending}
        onClick={() => {
          setConfirming(true);
        }}
      >
        Switch off
      </Button>
    </DisabledReason>
  ) : undefined;

  return (
    <Section
      title="Tailnet lock"
      description={
        <>
          Nodes sign each other&apos;s keys so a compromised control server cannot add a machine.
          The lock is switched on from a machine with <Code>tailscale lock init</Code>.
        </>
      }
      actions={actions}
      bodyClassName="p-0"
    >
      <DefinitionList items={buildDefinitions(lock)} />
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Switch off tailnet lock?"
        description="Every machine drops its lock state and key signatures."
        confirmLabel="Switch off"
        loading={disable.isPending}
        error={disable.isError ? errorMessage(disable.error) : undefined}
        onConfirm={() => {
          disable.mutate(undefined, {
            onSuccess: () => {
              setConfirming(false);
            },
          });
        }}
      />
    </Section>
  );
}
