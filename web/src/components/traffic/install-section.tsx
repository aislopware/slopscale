import type { ReactElement, ReactNode } from "react";

import { Code } from "~/components/ui/code.tsx";
import { CommandBox } from "~/components/ui/command-text.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";

const releasesUrl = "https://github.com/aislopware/slopscale/releases/latest";
const docsUrl = "https://aislopware.github.io/slopscale/ref/traffic/";
const linkClass =
  "text-kumo-link underline decoration-kumo-line underline-offset-2 hover:decoration-current";

function Step({
  number,
  title,
  children,
}: {
  readonly number: number;
  readonly title: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <SectionRow className="flex gap-3">
      <span className="flex h-lh w-5 shrink-0 items-center justify-center text-kumo-subtle tabular-nums">
        {number}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <span className="font-medium text-kumo-strong">{title}</span>
        {children}
      </div>
    </SectionRow>
  );
}

/**
 * How a machine becomes a gateway that reports. The agent needs no configuration: it finds the
 * server and proves which machine it runs on through the local tailscaled, so the steps are the
 * machine's role and one package.
 */
export function InstallSection(): ReactElement {
  return (
    <Section
      title="Add a gateway"
      description={
        <>
          The agent reads the gateway&apos;s connection table, so it sees every connection a machine
          opens through it.{" "}
          <a className={linkClass} href={docsUrl} target="_blank" rel="noreferrer">
            How it works
          </a>
        </>
      }
      bodyClassName="p-0"
    >
      <Step number={1} title="Pick a tagged Linux gateway">
        <p className="max-w-prose text-kumo-subtle">
          A tagged machine with approved exit or subnet routes, or one selected as an app connector.
          The server refuses reports from any other machine.
        </p>
      </Step>
      <Step number={2} title="Install the package on it">
        <p className="max-w-prose text-kumo-subtle">
          Download <Code>slopscale-flowd</Code> for the machine&apos;s architecture from the{" "}
          <a className={linkClass} href={releasesUrl} target="_blank" rel="noreferrer">
            latest release
          </a>{" "}
          and install it. It needs no configuration and starts at once.
        </p>
        <CommandBox command="sudo apt install ./slopscale-flowd_*_linux_amd64.deb" size="sm" />
      </Step>
      <Step number={3} title="Wait for the first report">
        <p className="max-w-prose text-kumo-subtle">
          The gateway shows up in the table above within a minute. If it does not, read{" "}
          <Code>journalctl -u slopscale-flowd</Code> on the machine.
        </p>
      </Step>
    </Section>
  );
}
