import { Button } from "@cloudflare/kumo/components/button";
import { CheckIcon, CopyIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { copyText } from "~/lib/copy.ts";

const resetAfterMs = 1500;

/** Icon-only copy control for values that are not plain text runs (use ClipboardText for those). */
export function CopyButton({
  value,
  label = "Copy",
  className,
}: {
  readonly value: string;
  readonly label?: string;
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
    <Button
      variant="ghost"
      shape="square"
      size="sm"
      {...(className === undefined ? {} : { className })}
      aria-label={label}
      title={copied ? "Copied" : label}
      icon={copied ? <CheckIcon className="text-kumo-success" /> : CopyIcon}
      onClick={() => {
        void copy();
      }}
    />
  );
}
