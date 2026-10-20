import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { PathIcon, PlusCircleIcon, ShieldCheckIcon } from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import type { Node } from "~/api/queries.ts";
import { StepCard } from "~/components/machines/add-step-card.tsx";
import { nodeName } from "~/lib/node.ts";

const iconSize = 16;

/** One of the next steps: an icon, what it is, and the button that does it, in an outline card. */
function NextStep({
  icon: StepIcon,
  title,
  description,
  action,
}: {
  readonly icon: Icon;
  readonly title: string;
  readonly description: string;
  readonly action: ReactNode;
}): ReactElement {
  return (
    <li className="flex items-start gap-3 rounded-lg px-4 py-3 ring ring-kumo-line">
      <StepIcon size={iconSize} aria-hidden className="mt-0.5 shrink-0 text-kumo-subtle" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="font-medium text-kumo-default">{title}</span>
        <span className="text-sm text-kumo-subtle">{description}</span>
      </div>
      <div className="shrink-0">{action}</div>
    </li>
  );
}

export function DoneStep({
  joined,
  onAnother,
}: {
  readonly joined: Node | undefined;
  readonly onAnother: () => void;
}): ReactElement {
  return (
    <StepCard
      title={joined === undefined ? "Key ready" : `${nodeName(joined)} added`}
      description={
        joined === undefined
          ? "The machine joins as soon as the command runs on it, while the key lasts. It then shows in the machine list."
          : "The machine is on the tailnet. What it may reach, and who may reach it, is up to the access rules."
      }
      footer={
        <>
          <LinkButton href="/machines" variant={joined === undefined ? "primary" : "ghost"}>
            Back to machines
          </LinkButton>
          {joined === undefined ? null : (
            <LinkButton href={`/machines/${joined.id}`} variant="primary">
              View machine details
            </LinkButton>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <h3 className="text-xs font-medium text-kumo-subtle">Next steps</h3>
        <ul className="flex flex-col gap-3">
          <NextStep
            icon={PlusCircleIcon}
            title="Add another machine"
            description="Each machine gets a key of its own, so one leaked key brings in one machine at most."
            action={
              <Button variant="ghost" size="sm" onClick={onAnother}>
                Add machine
              </Button>
            }
          />
          <NextStep
            icon={ShieldCheckIcon}
            title="Decide what it may reach"
            description="Access rules say which users and tags reach which machines and ports."
            action={
              <LinkButton href="/policy" variant="ghost" size="sm">
                Access rules
              </LinkButton>
            }
          />
          <NextStep
            icon={PathIcon}
            title="Route a network through it"
            description="Advertise a subnet or an exit node on the machine, then approve it under Routes."
            action={
              <LinkButton href="/routes" variant="ghost" size="sm">
                Routes
              </LinkButton>
            }
          />
        </ul>
      </div>
    </StepCard>
  );
}
