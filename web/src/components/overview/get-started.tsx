import { LinkButton } from "@cloudflare/kumo/components/button";
import { DownloadSimpleIcon, KeyIcon, PlusIcon, ShareNetworkIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { connectCommand, signInCommand } from "~/components/machines/connect.ts";
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

/** One machine on the sketch: a pill with its name and tailnet address. */
function SketchNode({
  name,
  address,
  className,
}: {
  readonly name: string;
  readonly address: string;
  readonly className: string;
}): ReactElement {
  return (
    <span
      className={`absolute flex flex-col gap-0.5 rounded-md bg-kumo-base px-2 py-1 text-left shadow-xs ring ring-kumo-line ${className}`}
    >
      <span className="flex items-center gap-1.5 text-[11px] font-medium text-kumo-default">
        <span className="size-1.5 rounded-full bg-kumo-success" />
        {name}
      </span>
      <span className="font-mono text-[10px] text-kumo-subtle">{address}</span>
    </span>
  );
}

/**
 * The empty tailnet's picture, as the Mesh empty state draws its nodes around Cloudflare: three
 * machines around this server, joined by dashed lines. It is decoration, so it is hidden from
 * assistive technology; the heading under it says the same thing in words.
 */
function TailnetSketch(): ReactElement {
  return (
    <div
      aria-hidden
      className="relative h-44 w-full max-w-[300px] overflow-hidden rounded-xl bg-kumo-base ring ring-kumo-line"
    >
      <span className="absolute top-1/2 left-1/2 size-28 -translate-1/2 rounded-full bg-kumo-brand/10 blur-xl" />
      <svg className="absolute inset-0 size-full" viewBox="0 0 300 176">
        <g className="stroke-kumo-line" strokeDasharray="3 3" strokeWidth="1">
          <line x1="150" y1="88" x2="150" y2="34" />
          <line x1="150" y1="88" x2="62" y2="104" />
          <line x1="150" y1="88" x2="236" y2="104" />
        </g>
      </svg>
      <span className="absolute top-1/2 left-1/2 flex size-9 -translate-1/2 items-center justify-center rounded-full bg-kumo-brand text-white shadow-sm">
        <ShareNetworkIcon className="size-4.5" weight="bold" />
      </span>
      <SketchNode name="laptop" address="100.64.0.1" className="top-3 left-1/2 -translate-x-1/2" />
      <SketchNode name="web-server" address="100.64.0.2" className="top-[88px] left-3" />
      <SketchNode name="exit-node" address="100.64.0.3" className="top-[88px] right-3" />
    </div>
  );
}

/**
 * The Mesh empty state's head: the sketch, what a machine is, and the one action that adds the
 * first. A caller who may not mint keys gets no button; the steps below say how to sign in.
 */
function EmptyTailnet({ withKey }: { readonly withKey: boolean }): ReactElement {
  return (
    <div className="flex flex-col items-center gap-4 rounded-lg bg-kumo-base px-6 py-8 text-center ring ring-kumo-line">
      <TailnetSketch />
      <h2 className="text-lg font-semibold text-kumo-default">No machines yet</h2>
      <p className="max-w-lg text-kumo-subtle">
        Machines are laptops, servers, phones and containers running the Tailscale client. Once one
        joins, it reaches every other machine the access rules allow over an encrypted mesh,
        directly wherever it can.
      </p>
      {withKey ? (
        <LinkButton href="/machines/add" variant="primary" icon={PlusIcon}>
          Add your first machine
        </LinkButton>
      ) : null}
    </div>
  );
}

export interface GetStartedProps {
  readonly me: Me;
}

/**
 * The steps between an empty list and its first machine, with the server URL already filled in. It
 * sits under the Mesh-style empty state while no machine has ever registered. A caller who may mint
 * pre-auth keys gets the key flow; anyone else, a member with no machine of their own included,
 * signs in on the machine instead.
 */
export function GetStarted({ me }: GetStartedProps): ReactElement {
  const withKey = can(me, "auth_keys");

  return (
    <div className="flex flex-col gap-4">
      <EmptyTailnet withKey={withKey} />
      <Section
        title="Connect your first machine"
        description={withKey ? "No machine has joined yet." : "No machine of yours has joined yet."}
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
        {withKey ? (
          <>
            <Step
              index={2}
              title="Create a pre-auth key"
              action={
                <LinkButton href="/keys/pre-auth/new" variant="secondary" icon={KeyIcon}>
                  Create key
                </LinkButton>
              }
            >
              <p className="text-kumo-subtle">
                The machine registers with the key instead of signing in, so it belongs to the user
                you pick.
              </p>
            </Step>
            <Step index={3} title="Point the machine at this server">
              <p className="text-kumo-subtle">
                Run this on the machine, with the key from step 2 in place of{" "}
                <Code className="text-kumo-warning">{keyPlaceholder}</Code>.
              </p>
              <CommandBox size="sm" command={connectCommand(keyPlaceholder)} />
            </Step>
          </>
        ) : (
          <Step index={2} title="Sign in on the machine">
            <p className="text-kumo-subtle">
              Run this on the machine and sign in as yourself. It joins as one of your machines and
              appears here.
            </p>
            <CommandBox size="sm" command={signInCommand()} />
          </Step>
        )}
      </Section>
    </div>
  );
}
