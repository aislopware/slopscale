import { Popover } from "@cloudflare/kumo/components/popover";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import type { AccessClass } from "~/components/access-graph/model.ts";
import { TagList, TagText, tagColours } from "~/components/ui/tag.tsx";

export const openDelay = 150;

/** The focus ring of everything in the map, inset so it shows on a cell at the panel's edge. */
export const focusClass =
  "outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:ring-inset";

/**
 * A class's machines, one hover or tap away from its header: the list opens under it and each
 * machine is a link to its own two lists.
 */
export function Members({
  group,
  onPick,
  children,
}: {
  readonly group: AccessClass;
  readonly onPick: (nodeId: string) => void;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <Popover>
      {children}
      <Popover.Content side="bottom" align="start" className="max-w-72 gap-1 p-3">
        <Popover.Title className="text-sm leading-5 font-medium">
          {group.label} · {group.detail}
        </Popover.Title>
        <ul className="-mx-1 flex max-h-64 flex-col overflow-y-auto px-1">
          {group.members.map((node) => (
            <li key={node.id}>
              <button
                type="button"
                className="w-full truncate rounded-sm px-1 py-0.5 text-left text-sm hover:text-kumo-link hover:underline"
                onClick={() => {
                  onPick(node.id);
                }}
              >
                {node.name}
              </button>
            </li>
          ))}
        </ul>
      </Popover.Content>
    </Popover>
  );
}

/**
 * A class on either axis: what its machines have in common and how many there are. A class of one
 * is named by its machine, and a tagged machine's tags are its owner line. Beside squares the two
 * go on one line, so a row is no taller than its cells.
 */
export function ClassHeader({
  group,
  onPick,
  dense = false,
  className,
}: {
  readonly group: AccessClass;
  readonly onPick: (nodeId: string) => void;
  readonly dense?: boolean;
  readonly className?: string;
}): ReactElement {
  const only = group.members.length === 1 ? group.members[0] : undefined;

  return (
    <Members group={group} onPick={onPick}>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        className={cn(
          "flex cursor-pointer text-left hover:bg-kumo-tint",
          dense
            ? "min-h-7 flex-row items-center gap-x-2 text-sm"
            : "min-h-11 flex-col justify-center gap-0.5",
          focusClass,
          className,
        )}
      >
        {group.tagged ? (
          <TagList tags={group.members[0]?.tags ?? []} size="sm" className="flex-nowrap" />
        ) : (
          <span className="block truncate text-kumo-default">{group.label}</span>
        )}
        {only !== undefined && only.tags.length > 0 ? (
          <TagList tags={only.tags} size="sm" className="flex-nowrap" />
        ) : (
          <span className="block truncate text-xs text-kumo-subtle">{group.detail}</span>
        )}
      </Popover.Trigger>
    </Members>
  );
}

/**
 * A column of the dense map, too narrow for a header to lie in: its label stands on end, read from
 * the bottom up, in the tag's own colour when the class is tagged. Its machines open under it like
 * a header's.
 */
export function ColumnLabel({
  group,
  onPick,
}: {
  readonly group: AccessClass;
  readonly onPick: (nodeId: string) => void;
}): ReactElement {
  const tag = group.tagged ? group.members[0]?.tags[0] : undefined;

  return (
    <Members group={group} onPick={onPick}>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        className={cn(
          "mx-auto block max-h-36 rotate-180 cursor-pointer rounded-xs px-0.5 pt-1.5 pb-2 text-xs leading-none text-kumo-subtle [writing-mode:vertical-rl] hover:bg-kumo-tint",
          focusClass,
        )}
        style={tag === undefined ? undefined : { color: tagColours(tag).color }}
      >
        {tag === undefined ? (
          <span className="block truncate">{group.label}</span>
        ) : (
          <TagText tag={tag} />
        )}
      </Popover.Trigger>
    </Members>
  );
}
