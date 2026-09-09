import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Collapsible } from "@cloudflare/kumo/components/collapsible";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import {
  ArrowCounterClockwiseIcon,
  BugIcon,
  CaretRightIcon,
  CompassIcon,
  LockKeyIcon,
  PlugsIcon,
  SignInIcon,
  WarningCircleIcon,
} from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import { useLocation, useMatches, useRouter } from "@tanstack/react-router";
import type { ErrorComponentProps } from "@tanstack/react-router";
import { useMemo } from "react";
import type { ReactElement, ReactNode } from "react";

import { meQuery } from "~/auth/me.ts";
import { ThemeToggle } from "~/components/layout/theme-toggle.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { Frame, FramePanel } from "~/components/ui/frame.tsx";
import { describeTrouble, pageNotFound, sentence } from "~/components/ui/trouble.ts";
import type { Trouble, TroubleKind } from "~/components/ui/trouble.ts";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

const iconSize = 22;
const caretSize = 12;

/** The mark at the top of the card: what kind of trouble, at a glance, in the tone it deserves. */
const tones: Record<TroubleKind, { readonly icon: Icon; readonly className: string }> = {
  unreachable: { icon: PlugsIcon, className: "bg-kumo-warning/10 text-kumo-warning" },
  session: { icon: SignInIcon, className: "bg-kumo-info/10 text-kumo-info" },
  forbidden: { icon: LockKeyIcon, className: "bg-kumo-warning/10 text-kumo-warning" },
  missing: { icon: CompassIcon, className: "bg-kumo-tint text-kumo-subtle" },
  server: { icon: WarningCircleIcon, className: "bg-kumo-danger/10 text-kumo-danger" },
  console: { icon: BugIcon, className: "bg-kumo-danger/10 text-kumo-danger" },
};

/** Whether the app layout is up around the page, so the error can sit in it with the sidebar. */
function useInShell(): boolean {
  return useMatches().some((match) => match.routeId === "/_app" && match.status === "success");
}

/** What to paste into a bug report: the message, the request and the page it happened on. */
function report(trouble: Trouble): string {
  const lines = [trouble.message];

  if (trouble.instance !== undefined) {
    lines.push(`Request: ${trouble.instance}`);
  }

  lines.push(`Page: ${globalThis.location.href}`);

  return lines.join("\n");
}

/** The message as the code produced it, folded away under the sentence written for people. */
function Details({ trouble }: { readonly trouble: Trouble }): ReactElement | null {
  // Nothing to fold away when the sentence above already is the message.
  const repeats = sentence(trouble.message) === trouble.description;

  if (trouble.message === "" || (repeats && trouble.instance === undefined)) {
    return null;
  }

  return (
    <Collapsible.Root>
      <Collapsible.Trigger className="group/details -mx-1 flex items-center gap-1 rounded-sm px-1 text-sm text-kumo-subtle hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none">
        <CaretRightIcon
          size={caretSize}
          weight="bold"
          className="transition-transform group-data-[panel-open]/details:rotate-90"
          aria-hidden
        />
        Details
      </Collapsible.Trigger>
      <Collapsible.Panel>
        <div className="mt-2 flex flex-col gap-1 rounded-md bg-kumo-tint px-3 py-2 text-sm">
          <CopyText
            value={trouble.message}
            copy={report(trouble)}
            label="Copy details"
            wrap
            display={<span className="whitespace-pre-wrap">{trouble.message}</span>}
          />
          {trouble.instance === undefined ? null : (
            <span className="font-mono text-[0.9em] text-kumo-subtle">
              Request {trouble.instance}
            </span>
          )}
        </div>
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}

/** The card: a mark, an eyebrow, the title, the sentence, the actions and the fold. */
function TroubleCard({
  trouble,
  actions,
}: {
  readonly trouble: Trouble;
  readonly actions: ReactNode;
}): ReactElement {
  const tone = tones[trouble.kind];
  const Mark = tone.icon;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-4">
        <span
          className={cn("flex size-10 items-center justify-center rounded-lg", tone.className)}
          aria-hidden
        >
          <Mark size={iconSize} weight="duotone" />
        </span>
        <div className="flex flex-col gap-1">
          <p className="font-mono text-xs text-kumo-subtle">{trouble.eyebrow}</p>
          <h1 className="text-xl font-semibold text-kumo-strong">{trouble.title}</h1>
          <p className="text-kumo-subtle">{trouble.description}</p>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2">{actions}</div>
      <Details trouble={trouble} />
    </div>
  );
}

/**
 * Where the card sits. Inside the app layout it is a framed card in the content area, so the
 * sidebar stays and the operator can simply go elsewhere. Outside it, when the layout itself could
 * not load, it is the login page's stage: the canvas, the theme toggle and the host underneath.
 */
function Stage({
  inShell,
  children,
}: {
  readonly inShell: boolean;
  readonly children: ReactNode;
}): ReactElement {
  if (inShell) {
    return (
      <div className="flex flex-1 items-center justify-center py-10">
        <Frame className="w-full max-w-md">
          <FramePanel className="px-6 py-6">{children}</FramePanel>
        </Frame>
      </div>
    );
  }

  return (
    <div className="flex min-h-dvh flex-col bg-kumo-canvas">
      <header className="flex items-center justify-end px-6 py-4">
        <ThemeToggle />
      </header>
      <main className="flex flex-1 items-center justify-center px-4 pb-24">
        <div className="flex w-full max-w-md flex-col gap-4">
          {/* In dark mode the canvas is nearly black, so the card needs a ring and a shadow of its
              own to read as a surface. */}
          <Frame className="shadow-lg ring-kumo-line">
            <FramePanel className="px-6 py-6">{children}</FramePanel>
          </Frame>
          <p className="text-center text-xs text-kumo-subtle">
            <span className="font-mono text-[0.9em]">{globalThis.location.host}</span>
          </p>
        </div>
      </main>
    </div>
  );
}

function OverviewButton({ primary = false }: { readonly primary?: boolean }): ReactElement {
  return (
    <LinkButton href="/" variant={primary ? "primary" : "secondary"}>
      Back to overview
    </LinkButton>
  );
}

function BackButton(): ReactElement {
  const router = useRouter();

  return (
    <Button
      variant="secondary"
      onClick={() => {
        router.history.back();
      }}
    >
      Go back
    </Button>
  );
}

/**
 * Sign in and come back here: the router's address, which is what the login page navigates to. The
 * guards' cached answer said the operator was signed in, so it goes before the sign-in page asks
 * again, or it would send the operator straight back to this page.
 */
function SignInButton(): ReactElement {
  const here = useLocation({ select: (location) => location.href });
  const queryClient = useQueryClient();

  return (
    <LinkButton
      href={`/login?redirect=${encodeURIComponent(here)}`}
      variant="primary"
      icon={SignInIcon}
      onClick={() => {
        queryClient.removeQueries({ queryKey: meQuery.queryKey });
      }}
    >
      Sign in
    </LinkButton>
  );
}

/** The actions a kind of trouble calls for, primary rightmost. */
function Actions({
  trouble,
  inShell,
  retry,
}: {
  readonly trouble: Trouble;
  readonly inShell: boolean;
  readonly retry: () => void;
}): ReactElement {
  if (trouble.kind === "session") {
    return <SignInButton />;
  }

  if (trouble.kind === "forbidden" || trouble.kind === "missing") {
    return (
      <>
        <BackButton />
        <OverviewButton primary />
      </>
    );
  }

  return (
    <>
      {inShell ? <OverviewButton /> : null}
      <Button variant="primary" icon={ArrowCounterClockwiseIcon} onClick={retry}>
        Try again
      </Button>
    </>
  );
}

/** The page for anything a route threw: the default error component of the router. */
export function RouteError({ error, reset }: ErrorComponentProps): ReactElement {
  const router = useRouter();
  const inShell = useInShell();
  const trouble = useMemo(() => describeTrouble(error), [error]);

  useBreadcrumb(inShell ? trouble.title : null);

  return (
    <Stage inShell={inShell}>
      <TroubleCard
        trouble={trouble}
        actions={
          <Actions
            trouble={trouble}
            inShell={inShell}
            retry={() => {
              reset();
              void router.invalidate();
            }}
          />
        }
      />
    </Stage>
  );
}

/** The page for an address the console does not know: the default not-found component. */
export function RouteNotFound(): ReactElement {
  const inShell = useInShell();

  useBreadcrumb(inShell ? pageNotFound.title : null);

  return (
    <Stage inShell={inShell}>
      <TroubleCard
        trouble={pageNotFound}
        actions={
          <>
            <BackButton />
            <OverviewButton primary />
          </>
        }
      />
    </Stage>
  );
}
