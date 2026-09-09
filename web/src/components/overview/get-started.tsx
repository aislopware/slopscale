import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { DownloadSimpleIcon, KeyIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { connectCommand } from "~/components/machines/connect.ts";
import { Code } from "~/components/ui/code.tsx";
import { CommandBox } from "~/components/ui/command-text.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";

const downloadUrl = "https://tailscale.com/download";
/** Stands in for the secret from step 2, so the command reads exactly as it will be run. */
const keyPlaceholder = "<key>";

function Step({
  index,
  title,
  children,
  action,
}: {
  readonly index: number;
  readonly title: string;
  readonly children: ReactNode;
  readonly action?: ReactNode;
}): ReactElement {
  return (
    <SectionRow className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="flex min-w-0 items-start gap-3">
        <span className="flex h-lh items-center">
          <span className="flex size-5 shrink-0 items-center justify-center rounded-md bg-kumo-tint text-xs font-medium text-kumo-subtle ring ring-kumo-hairline">
            {index}
          </span>
        </span>
        <div className="flex min-w-0 flex-col gap-1.5">
          <p className="font-medium text-kumo-strong">{title}</p>
          {children}
        </div>
      </div>
      {action === undefined ? null : <div className="shrink-0">{action}</div>}
    </SectionRow>
  );
}

export interface GetStartedProps {
  readonly me: Me;
  readonly onAddMachine: () => void;
}

/**
 * The three steps between an empty tailnet and its first machine, with the server URL already
 * filled in. It replaces the recent activity list while no machine has ever registered.
 */
export function GetStarted({ me, onAddMachine }: GetStartedProps): ReactElement {
  return (
    <Section
      title="Connect your first machine"
      description="No machine has joined yet."
      bodyClassName="p-0"
    >
      <Step
        index={1}
        title="Install Tailscale"
        action={
          <LinkButton href={downloadUrl} external variant="secondary" icon={DownloadSimpleIcon}>
            Download
          </LinkButton>
        }
      >
        <p className="text-kumo-subtle">
          Install the Tailscale client on the machine you want on the tailnet.
        </p>
      </Step>
      <Step
        index={2}
        title="Create a pre-auth key"
        action={
          <Button
            variant="secondary"
            icon={KeyIcon}
            disabled={!can(me, "auth_keys")}
            onClick={onAddMachine}
          >
            Create key
          </Button>
        }
      >
        <p className="text-kumo-subtle">
          The machine registers with the key instead of signing in, so it belongs to the user you
          pick.
        </p>
      </Step>
      <Step index={3} title="Point the machine at this server">
        <p className="text-kumo-subtle">
          Run this on the machine, with the key from step 2 in place of{" "}
          <Code className="text-kumo-warning">{keyPlaceholder}</Code>.
        </p>
        <CommandBox size="sm" command={connectCommand(keyPlaceholder)} />
      </Step>
    </Section>
  );
}
