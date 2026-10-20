import { Button } from "@cloudflare/kumo/components/button";
import { Loader } from "@cloudflare/kumo/components/loader";
import { cn } from "@cloudflare/kumo/utils";
import { CheckCircleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement } from "react";

import { nodesQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { DoneStep } from "~/components/machines/add-done-step.tsx";
import { OwnerStep } from "~/components/machines/add-owner-step.tsx";
import { Folded, StepCard } from "~/components/machines/add-step-card.tsx";
import { JoinCommand } from "~/components/machines/join-command.tsx";
import { nodeName } from "~/lib/node.ts";

/** How often the install step looks for the machine the key brought in. */
const joinPollMs = 2000;
const iconSize = 16;

export type WizardStep = "owner" | "install" | "done";

const steps: readonly { readonly id: WizardStep; readonly label: string }[] = [
  { id: "owner", label: "Who it belongs to" },
  { id: "install", label: "Install and connect" },
  { id: "done", label: "Done" },
];

/** What the first step settled, for the faded card it leaves above the next one. */
interface Minted {
  readonly key: string;
  readonly summary: string;
  /** The machines that existed when the key was made; a new id after that is the one it added. */
  readonly before: ReadonlySet<string>;
}

/**
 * Adding a machine as Cloudflare's Mesh adds a node: a column of steps with a dot stepper beside
 * it, each finished step folded into a faded card above the current one. First whose the machine is
 * (a single-use pre-auth key for that user, with its tags and options), then the command per
 * platform with a footer that waits for the machine to show up, then what to do next.
 */
export function AddMachineWizard({ me }: { readonly me: Me }): ReactElement {
  const [step, setStep] = useState<WizardStep>("owner");
  const [minted, setMinted] = useState<Minted | null>(null);
  const nodes = useQuery({
    ...nodesQuery,
    refetchInterval: step === "install" ? joinPollMs : false,
  });
  const joined =
    minted === null
      ? undefined
      : nodes.data?.nodes.find((candidate) => !minted.before.has(candidate.id));

  return (
    <div className="grid justify-center gap-8 lg:grid-cols-[minmax(0,35rem)_12rem]">
      <ol
        aria-label="Steps"
        className="flex gap-4 max-lg:flex-wrap lg:sticky lg:top-[calc(var(--app-header-height,0px)+2rem)] lg:order-last lg:flex-col lg:gap-3 lg:self-start"
      >
        {steps.map((item) => (
          <li
            key={item.id}
            aria-current={item.id === step ? "step" : undefined}
            className={cn(
              "flex items-center gap-2.5 text-sm",
              item.id === step ? "font-medium text-kumo-default" : "text-kumo-subtle",
            )}
          >
            <span
              aria-hidden
              className={cn(
                "size-1.5 rounded-full ring ring-current",
                item.id === step && "bg-current",
              )}
            />
            {item.label}
          </li>
        ))}
      </ol>
      <div className="flex min-w-0 flex-col gap-4">
        {step === "owner" ? (
          <OwnerStep
            me={me}
            onMinted={(key, summary) => {
              setMinted({
                key,
                summary,
                before: new Set(nodes.data?.nodes.map((candidate) => candidate.id)),
              });
              setStep("install");
            }}
          />
        ) : (
          <Folded title="Who it belongs to" lines={[minted?.summary ?? ""]} />
        )}
        {step === "install" && minted !== null ? (
          <InstallStep
            authKey={minted.key}
            joined={joined}
            onContinue={() => {
              setStep("done");
            }}
          />
        ) : null}
        {step === "done" ? (
          <>
            <Folded
              title="Install and connect"
              settled={joined !== undefined}
              lines={[joined === undefined ? "Not connected yet" : `${nodeName(joined)} connected`]}
            />
            <DoneStep
              joined={joined}
              onAnother={() => {
                setMinted(null);
                setStep("owner");
              }}
            />
          </>
        ) : null}
      </div>
    </div>
  );
}

/**
 * The command per platform, and a footer that watches for the machine: a spinner while nothing has
 * joined, the machine's name once something has, and Continue held back until then. "I'll connect
 * later" moves on without it, since the key stays good until it expires.
 */
function InstallStep({
  authKey,
  joined,
  onContinue,
}: {
  readonly authKey: string;
  readonly joined: Node | undefined;
  readonly onContinue: () => void;
}): ReactElement {
  return (
    <StepCard
      title="Install and connect"
      description="Run this on the machine. It installs Tailscale where it can, points it at this server and joins with the key. The key is shown only once."
      footer={
        <>
          <output className="mr-auto flex items-center gap-2 text-sm text-kumo-subtle">
            {joined === undefined ? (
              <>
                <Loader size="sm" />
                Waiting for connection…
              </>
            ) : (
              <>
                <CheckCircleIcon
                  size={iconSize}
                  weight="fill"
                  aria-hidden
                  className="text-kumo-success"
                />
                {`${nodeName(joined)} joined`}
              </>
            )}
          </output>
          {joined === undefined ? (
            <Button variant="ghost" onClick={onContinue}>
              I’ll connect later
            </Button>
          ) : null}
          <Button variant="primary" disabled={joined === undefined} onClick={onContinue}>
            Continue
          </Button>
        </>
      }
    >
      <JoinCommand authKey={authKey} />
    </StepCard>
  );
}
