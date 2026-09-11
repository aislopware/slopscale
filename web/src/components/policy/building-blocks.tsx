import { Collapsible } from "@cloudflare/kumo/components/collapsible";
import { cn } from "@cloudflare/kumo/utils";
import { CaretDownIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { PolicyBlocks } from "~/components/policy/blocks.ts";
import { Frame, FramePanel } from "~/components/ui/frame.tsx";
import { Tag } from "~/components/ui/tag.tsx";

const caretSize = 14;

function BlockRow({
  label,
  names,
  empty,
  tags = false,
  onSelect,
}: {
  readonly label: string;
  readonly names: readonly string[];
  readonly empty: string;
  /** Draws the names as tag chips, the way the rest of the console shows a tag. */
  readonly tags?: boolean;
  readonly onSelect: (name: string) => void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-1.5 px-4 py-3 not-first:border-t not-first:border-kumo-hairline">
      <span className="text-xs text-kumo-subtle">{label}</span>
      {names.length === 0 ? (
        <span className="text-kumo-subtle">{empty}</span>
      ) : (
        <div className={cn("flex items-start", tags ? "flex-wrap gap-1" : "flex-col gap-0.5")}>
          {names.map((name) => (
            <button
              key={name}
              type="button"
              title={`Find ${name} in the policy`}
              className={cn(
                "rounded-sm text-left focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
                tags
                  ? "rounded-md hover:opacity-80"
                  : "font-mono text-[0.9em] [overflow-wrap:anywhere] text-kumo-default hover:underline",
              )}
              onClick={() => {
                onSelect(name);
              }}
            >
              {tags ? <Tag tag={name} size="sm" /> : name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * The names the draft in the editor defines or refers to. It reads the editor text, not the saved
 * policy, so an operator sees a group appear as they type it. The panel folds away because on a
 * narrow window the editor wants the whole width; clicking a name jumps the editor to it.
 */
export function BuildingBlocks({
  blocks,
  open,
  onOpenChange,
  onSelect,
}: {
  readonly blocks: PolicyBlocks;
  readonly open: boolean;
  readonly onOpenChange: (next: boolean) => void;
  readonly onSelect: (name: string) => void;
}): ReactElement {
  return (
    <Collapsible.Root
      open={open}
      onOpenChange={onOpenChange}
      className="hidden h-fit min-w-0 xl:block"
      render={<section />}
    >
      <Frame>
        <Collapsible.Trigger className="flex w-full items-center justify-between gap-2 rounded-lg px-4 py-2 text-left hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none">
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="font-semibold whitespace-nowrap text-kumo-strong">
              Building blocks
            </span>
            {open ? (
              <span className="text-xs text-kumo-subtle">
                {blocks.valid
                  ? "Names in the draft"
                  : "Read from the text because the draft does not parse"}
              </span>
            ) : null}
          </span>
          <CaretDownIcon
            size={caretSize}
            aria-hidden
            className="shrink-0 text-kumo-subtle transition-transform duration-100 ease-out [[data-panel-open]_&]:rotate-180"
          />
        </Collapsible.Trigger>
        <Collapsible.Panel>
          <FramePanel>
            <BlockRow
              label="Groups"
              names={blocks.groups}
              empty="No groups yet"
              onSelect={onSelect}
            />
            <BlockRow
              label="Tags"
              names={blocks.tags}
              empty="No tags yet"
              tags
              onSelect={onSelect}
            />
            <BlockRow
              label="Autogroups"
              names={blocks.autogroups}
              empty="No autogroups yet"
              onSelect={onSelect}
            />
          </FramePanel>
        </Collapsible.Panel>
      </Frame>
    </Collapsible.Root>
  );
}
