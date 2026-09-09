import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { ArrowLeftIcon, TerminalWindowIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { api } from "~/api/client.ts";
import { nodeSshUsernamesQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import { SSHTerminal } from "~/components/ssh/terminal.tsx";
import { UsernameField, usernamePrefillStep } from "~/components/ssh/username-field.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { Callout } from "~/components/ui/callout.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Status } from "~/components/ui/status.tsx";
import type { Tone } from "~/components/ui/status.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { nodeName } from "~/lib/node.ts";
import type { SSHSessionState, SSHStatus } from "~/tsconnect/types.ts";
import { useSSHSession } from "~/tsconnect/use-ssh-session.ts";
import type { SSHSession } from "~/tsconnect/use-ssh-session.ts";
import type { IPN, IPNSSHSession } from "~/tsconnect/wasm-js.d.ts";

export const Route = createFileRoute("/_app/machines/$nodeId_/ssh")({
  loader: async ({ context, params }) => {
    await context.queryClient.query(
      api.queryOptions("get", "/api/v1/node/{nodeId}", {
        params: { path: { nodeId: params.nodeId } },
      }),
    );
  },
  component: SSHPage,
});

/** One array for "the machine has not answered", so the prefill does not re-run on every render. */
const noUsernames: readonly string[] = [];

const statuses: Record<SSHStatus, { readonly tone: Tone; readonly label: string }> = {
  "loading-client": { tone: "neutral", label: "Loading client" },
  "joining-tailnet": { tone: "info", label: "Joining tailnet" },
  connecting: { tone: "info", label: "Connecting" },
  connected: { tone: "success", label: "Connected" },
  done: { tone: "neutral", label: "Closed" },
  error: { tone: "danger", label: "Failed" },
};

function resolvePageTitle(node: Node | undefined, session: SSHSession | null): string {
  if (node !== undefined) {
    return nodeName(node);
  }
  if (session !== null) {
    return session.target.name;
  }
  return "SSH session";
}

/** The wait shown in place of the terminal, or null once the client can dial. */
function waitingMessage(status: SSHStatus): string | null {
  if (status === "loading-client") {
    return "Loading the in-browser client…";
  }
  if (status === "joining-tailnet") {
    return "Joining the tailnet…";
  }
  return null;
}

/** The field holds a draft that only Reconnect commits, so it is locked while a session is up. */
function usernameEditable(status: SSHStatus): boolean {
  return status === "done" || status === "error";
}

/** What the client is doing right now: the tailnet state while joining, the dialling step after. */
function sessionDetail(state: SSHSessionState): string | null {
  if (state.status === "joining-tailnet") {
    return state.ipnState;
  }
  if (state.status === "connecting") {
    return state.progressMessage;
  }
  return null;
}

function SSHPage(): ReactElement {
  const { nodeId } = Route.useParams();
  const detail = useQuery(
    api.queryOptions("get", "/api/v1/node/{nodeId}", {
      params: { path: { nodeId } },
    }),
  );
  const node = detail.data?.node;
  // A hint from the machine itself, and only ever a hint: the SSH policy decides, so a refusal or
  // an empty answer just leaves the field as it was.
  const hints = useQuery({
    ...nodeSshUsernamesQuery(nodeId),
    enabled: node?.online ?? false,
    retry: false,
  });
  const usernames = hints.data?.usernames ?? noUsernames;

  const {
    state,
    session,
    username,
    setUsername,
    connectionUsername,
    ipn,
    sessionKey,
    reconnect,
    disconnect,
    onConnectionProgress,
    onConnected,
    onDone,
    registerSession,
  } = useSSHSession(nodeId);

  const title = resolvePageTitle(node, session);
  useBreadcrumb(`${title} (SSH)`);

  const waiting = waitingMessage(state.status);
  const note = sessionDetail(state);

  // The field is filled once at most, from the server's session or from the machine's suggestion,
  // and the ref remembers that the chance has been used: anything that reached the field, including
  // the operator's own typing, uses it up, so a field cleared afterwards stays cleared.
  const prefill = useRef(false);

  useEffect(() => {
    const step = usernamePrefillStep(username, usernames, prefill.current);

    prefill.current = step.consumed;

    if (step.insert !== null) {
      setUsername(step.insert);
    }
  }, [username, usernames, setUsername]);

  const handleSubmit = (event: SubmitEvent<HTMLFormElement>): void => {
    event.preventDefault();
    if (state.status === "done" || state.status === "error") {
      reconnect();
    }
  };

  return (
    <div className="flex h-[calc(100svh-9rem)] flex-col gap-4">
      <PageHeader
        eyebrow={
          <LinkButton href={`/machines/${nodeId}`} variant="ghost" size="sm" icon={ArrowLeftIcon}>
            Machine details
          </LinkButton>
        }
        title={title}
        meta={
          <>
            <Status tone={statuses[state.status].tone}>{statuses[state.status].label}</Status>
            {session === null ? null : (
              <>
                <span aria-hidden>·</span>
                <span className="font-mono">{session.target.dnsName || session.target.name}</span>
              </>
            )}
            {note === null ? null : (
              <>
                <span aria-hidden>·</span>
                <span>{note}</span>
              </>
            )}
          </>
        }
        actions={
          <form className="flex items-center gap-2" onSubmit={handleSubmit}>
            <UsernameField
              value={username}
              suggestions={usernames}
              disabled={!usernameEditable(state.status)}
              onValueChange={setUsername}
            />
            <ActionButton status={state.status} onDisconnect={disconnect} onReconnect={reconnect} />
          </form>
        }
      />

      {state.status === "error" ? (
        <SessionError notBuilt={state.notBuilt} error={state.error} />
      ) : (
        <div className="flex flex-1 flex-col overflow-hidden rounded-lg bg-kumo-base ring ring-kumo-line">
          <TerminalPane
            waiting={waiting}
            ipn={ipn}
            session={session}
            username={connectionUsername}
            sessionKey={sessionKey}
            onConnectionProgress={onConnectionProgress}
            onConnected={onConnected}
            onDone={onDone}
            registerSession={registerSession}
          />
        </div>
      )}
    </div>
  );
}

interface TerminalPaneProps {
  readonly waiting: string | null;
  readonly ipn: IPN | null;
  readonly session: SSHSession | null;
  readonly username: string;
  readonly sessionKey: number;
  readonly onConnectionProgress: (message: string) => void;
  readonly onConnected: () => void;
  readonly onDone: () => void;
  readonly registerSession: (session: IPNSSHSession | null) => void;
}

/**
 * The terminal, once the client is on the tailnet. A new `sessionKey` remounts it, which is how
 * Reconnect asks for a second SSH session over the same client.
 */
function TerminalPane({
  waiting,
  ipn,
  session,
  username,
  sessionKey,
  onConnectionProgress,
  onConnected,
  onDone,
  registerSession,
}: TerminalPaneProps): ReactElement {
  if (waiting !== null || ipn === null || session === null) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-kumo-subtle">
        {waiting ?? "Joining the tailnet…"}
      </div>
    );
  }

  return (
    <SSHTerminal
      key={sessionKey}
      ipn={ipn}
      host={session.target.name}
      username={username}
      onConnectionProgress={onConnectionProgress}
      onConnected={onConnected}
      onDone={onDone}
      registerSession={registerSession}
    />
  );
}

function SessionError({
  notBuilt,
  error,
}: {
  readonly notBuilt: boolean;
  readonly error: string | null;
}): ReactElement {
  if (notBuilt) {
    return (
      <div className="flex flex-1 items-center justify-center rounded-lg bg-kumo-base p-8 ring ring-kumo-line">
        <Empty
          size="sm"
          className={tableEmptyClass}
          icon={<TerminalWindowIcon size={emptyIconSize} />}
          title="The in-browser client was not built"
          description="Run make wasm from the repository root, then reload this page."
        />
      </div>
    );
  }

  return (
    <Callout
      tone="error"
      title="This machine cannot be reached over SSH"
      description={error ?? "The session could not be started."}
    />
  );
}

function ActionButton({
  status,
  onDisconnect,
  onReconnect,
}: {
  readonly status: SSHStatus;
  readonly onDisconnect: () => void;
  readonly onReconnect: () => void;
}): ReactElement {
  if (status === "connected") {
    return (
      <Button variant="secondary" onClick={onDisconnect}>
        Disconnect
      </Button>
    );
  }

  if (status === "done" || status === "error") {
    return (
      <Button variant="primary" onClick={onReconnect}>
        Reconnect
      </Button>
    );
  }

  return (
    <Button variant="primary" loading disabled>
      Connect
    </Button>
  );
}
