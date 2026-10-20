import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { cn } from "@cloudflare/kumo/utils";
import { CheckIcon, PencilSimpleIcon, XIcon } from "@phosphor-icons/react";
import { useId, useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Node } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { mayManageNode } from "~/components/machines/owner.ts";
import { KeyValueList } from "~/components/ui/definition-list.tsx";
import type { KeyValue } from "~/components/ui/definition-list.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { dnsLabelIssue } from "~/lib/dns-label.ts";
import { nodeName } from "~/lib/node.ts";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

const iconSize = 14;

/** Puts the caret in the rename box as the pencil swaps it in, as a dialog's first field gets it. */
function focusOnMount(element: HTMLInputElement | null): void {
  element?.focus();
}

/**
 * The machine's identity, as the details rail of a Zero Trust device profile or a Mesh node: the
 * name with a pencil that renames it in place, the ID and the tailnet addresses each with a copy
 * button, and when it joined, was last seen and expires. It sits in the sticky rail beside the
 * cards, so the facts an operator copies stay in reach while the page scrolls.
 */
export function MachineDetails({
  node,
  me,
}: {
  readonly node: Node;
  readonly me: Me;
}): ReactElement {
  return (
    <Section title="Machine details" bodyClassName="py-1">
      <NameRow node={node} editable={mayManageNode(me, node)} />
      <KeyValueList className="border-t border-kumo-line" items={facts(node)} />
    </Section>
  );
}

function facts(node: Node): KeyValue[] {
  const addresses = node.ipAddresses.toSorted(
    (left, right) => Number(left.includes(":")) - Number(right.includes(":")),
  );

  return [
    {
      key: "id",
      label: "Machine ID",
      value: <span className="font-mono">{node.id}</span>,
      copy: node.id,
      copyLabel: "Copy machine ID",
    },
    ...addresses.map((address) => {
      const family = address.includes(":") ? "IPv6" : "IPv4";

      return {
        key: address,
        label: `${family} address`,
        value: <span className="font-mono">{address}</span>,
        copy: address,
        copyLabel: `Copy ${family} address`,
      };
    }),
    {
      key: "seen",
      label: "Last seen",
      value: node.online ? "Connected now" : <RelativeTime value={node.lastSeen} />,
    },
    { key: "created", label: "Created", value: <Timestamp value={node.createdAt} /> },
    {
      key: "expiry",
      label: "Key expiry",
      value: parseTime(node.expiry) === null ? "Never" : <Timestamp value={node.expiry} />,
    },
  ];
}

function Timestamp({ value }: { readonly value: string | null }): ReactElement {
  const date = parseTime(value);

  return date === null ? (
    <span className="text-kumo-subtle">Unknown</span>
  ) : (
    <span>{formatAbsolute(date)}</span>
  );
}

/**
 * The name, and the pencil that swaps it for a small input with a tick and a cross, as the
 * dashboard renames a tunnel from its details rail. Saving an unchanged or empty name just closes
 * the input; a name that cannot be a DNS label says why under it before anything is sent.
 */
function NameRow({
  node,
  editable,
}: {
  readonly node: Node;
  readonly editable: boolean;
}): ReactElement {
  const [editing, setEditing] = useState(false);
  const name = nodeName(node);

  return (
    <div className="grid gap-1 px-4 py-3">
      <span className="text-sm text-kumo-subtle">Name</span>
      {editing ? (
        <RenameForm
          node={node}
          onDone={() => {
            setEditing(false);
          }}
        />
      ) : (
        <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-kumo-default">
          <span className="min-w-0 truncate">{name}</span>
          {editable ? (
            <Button
              variant="ghost"
              shape="square"
              size="sm"
              aria-label="Rename machine"
              title="Rename machine"
              className="shrink-0 text-kumo-subtle"
              onClick={() => {
                setEditing(true);
              }}
            >
              <PencilSimpleIcon size={iconSize} aria-hidden />
            </Button>
          ) : null}
        </div>
      )}
    </div>
  );
}

function RenameForm({
  node,
  onDone,
}: {
  readonly node: Node;
  readonly onDone: () => void;
}): ReactElement {
  const current = nodeName(node);
  const [draft, setDraft] = useState(current);
  const [touched, setTouched] = useState(false);
  const { rename } = useNodeMutations();
  const next = draft.trim();
  const issue = next === "" ? null : dnsLabelIssue(next);
  const shown = touched ? issue : null;
  const failed = rename.isError ? errorMessage(rename.error) : null;
  const invalid = shown !== null || failed !== null;
  const errorId = useId();

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    // Nothing to send: an empty box puts the name back, as the dashboard's inline rename does.
    if (next === "" || next === current) {
      onDone();

      return;
    }

    if (issue !== null) {
      return;
    }

    rename.mutate(
      { params: { path: { nodeId: node.id, newName: next } } },
      {
        onSuccess: () => {
          toast.success("Machine renamed");
          onDone();
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        <Input
          size="sm"
          aria-label="Machine name"
          className={cn("min-w-0 flex-1", invalid && "!ring-kumo-danger")}
          value={draft}
          spellCheck={false}
          ref={focusOnMount}
          aria-invalid={invalid}
          {...(invalid ? { "aria-describedby": errorId } : {})}
          onChange={(event) => {
            setDraft(event.target.value);
            rename.reset();
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              onDone();
            }
          }}
        />
        <Button
          type="submit"
          variant="primary"
          shape="square"
          size="sm"
          aria-label="Save name"
          loading={rename.isPending}
        >
          <CheckIcon size={iconSize} aria-hidden />
        </Button>
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          aria-label="Cancel rename"
          onClick={onDone}
        >
          <XIcon size={iconSize} aria-hidden />
        </Button>
      </div>
      {shown === null && failed === null ? null : (
        <p id={errorId} role="alert" className="text-sm leading-snug text-kumo-danger">
          {shown ?? failed}
        </p>
      )}
    </form>
  );
}
