import { cn } from "@cloudflare/kumo/utils";
import { CheckIcon, CopyIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { copyText } from "~/lib/copy.ts";

const iconSize = 12;
const resetAfterMs = 1500;

/**
 * A monospace value that copies itself when clicked: the copy icon only appears on hover, so table
 * rows stay as tall as their text instead of as tall as a button.
 */
export function CopyText({
  value,
  className,
}: {
  readonly value: string;
  readonly className?: string;
}): ReactElement {
  const [copied, setCopied] = useState(false);

  async function copy(): Promise<void> {
    if (await copyText(value)) {
      setCopied(true);
      setTimeout(() => {
        setCopied(false);
      }, resetAfterMs);
    }
  }

  return (
    <button
      type="button"
      title={copied ? "Copied" : `Copy ${value}`}
      onClick={() => {
        void copy();
      }}
      className={cn(
        "group/copy -mx-1 inline-flex max-w-full items-center gap-1 rounded-sm px-1 font-mono text-[0.9em] hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
        className,
      )}
    >
      <span className="truncate">{value}</span>
      {copied ? (
        <CheckIcon size={iconSize} className="shrink-0 text-kumo-success" />
      ) : (
        <CopyIcon
          size={iconSize}
          aria-hidden
          className="shrink-0 opacity-0 group-hover/copy:opacity-100"
        />
      )}
    </button>
  );
}
