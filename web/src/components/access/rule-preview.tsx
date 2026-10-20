import {
  ArrowsLeftRightIcon,
  CheckCircleIcon,
  ClockIcon,
  PauseCircleIcon,
  PlugsConnectedIcon,
  ShieldCheckIcon,
  UsersThreeIcon,
} from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Group, Posture } from "~/api/queries.ts";
import { groupName, protocolSummary } from "~/components/access/model.ts";
import { postureName } from "~/components/access/posture-model.ts";
import type { RuleDraft } from "~/components/access/rule-draft.ts";
import type { PreviewColumn, PreviewItem } from "~/components/policy/rule-builder.tsx";
import { formatDate, fromLocalInput, parseTime } from "~/lib/time.ts";

/** What the preview needs besides the draft to name what the draft points at. */
export interface PreviewContext {
  readonly groups: readonly Group[];
  readonly postures: readonly Posture[];
  readonly toOwnMachines: boolean;
}

function names(ids: readonly string[], lookup: (id: string) => string): string {
  return ids.map((id) => lookup(id)).join(", ");
}

function Pending({ children }: { readonly children: string }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
}

function sourceItems(draft: RuleDraft, { groups, postures }: PreviewContext): PreviewItem[] {
  const sources: PreviewItem[] =
    draft.sources.length === 0
      ? [{ key: "none", icon: UsersThreeIcon, content: <Pending>No source group yet</Pending> }]
      : draft.sources.map((id) => ({
          key: id,
          icon: UsersThreeIcon,
          content: groupName(groups, id),
        }));

  if (draft.postures.length === 0) {
    return sources;
  }

  return [
    ...sources,
    {
      key: "postures",
      icon: ShieldCheckIcon,
      label: "Device posture",
      content: names(draft.postures, (id) => postureName(postures, id)),
    },
  ];
}

function trafficItems(draft: RuleDraft, { groups }: PreviewContext): PreviewItem[] {
  return [
    {
      key: "destination",
      icon: UsersThreeIcon,
      label: "Destination",
      content:
        draft.destinations.length === 0 ? (
          <Pending>No destination group yet</Pending>
        ) : (
          <>
            <span className="text-kumo-subtle">Group in </span>
            {names(draft.destinations, (id) => groupName(groups, id))}
          </>
        ),
    },
    {
      key: "protocol",
      icon: PlugsConnectedIcon,
      label: "Protocol and ports",
      content: protocolSummary({ protocol: draft.protocol, ports: draft.ports.trim() }),
    },
  ];
}

function outcomeItems(draft: RuleDraft, { toOwnMachines }: PreviewContext): PreviewItem[] {
  const expires = draft.expiring ? parseTime(fromLocalInput(draft.expires) ?? null) : null;

  return [
    { key: "allow", icon: CheckCircleIcon, content: "Allow" },
    ...(draft.bidirectional && !toOwnMachines
      ? [{ key: "both", icon: ArrowsLeftRightIcon, content: "Both directions" }]
      : []),
    ...(expires === null
      ? []
      : [{ key: "expires", icon: ClockIcon, content: `Until ${formatDate(expires)}` }]),
    ...(draft.enabled
      ? []
      : [{ key: "inactive", icon: PauseCircleIcon, content: "Saved as inactive" }]),
  ];
}

/** The draft in the preview's three columns: what comes in, what it reaches, what happens. */
export function previewColumns(draft: RuleDraft, context: PreviewContext): PreviewColumn[] {
  return [
    { title: "Sources", items: sourceItems(draft, context) },
    { title: "Traffic contains", items: trafficItems(draft, context) },
    { title: "Outcome", items: outcomeItems(draft, context) },
  ];
}
