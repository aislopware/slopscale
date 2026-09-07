import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { ClipboardText } from "@cloudflare/kumo/components/clipboard-text";
import { CheckCircleIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { DialogFooter } from "~/components/ui/dialog.tsx";

/**
 * The secret a create dialog just minted, cleared as soon as the dialog opens again so a stale key
 * is never revealed twice. The dialog's title changes with it, so it cannot live inside the body.
 */
export function useCreatedKey(open: boolean): [string | null, (key: string) => void] {
  const [created, setCreated] = useState<string | null>(null);
  const [wasOpen, setWasOpen] = useState(open);

  if (wasOpen !== open) {
    setWasOpen(open);

    if (open) {
      setCreated(null);
    }
  }

  return [created, setCreated];
}

/** The one and only time a secret is readable: shown after the key is minted. */
export function CreatedKey({
  value,
  note,
  command,
  onDone,
}: {
  readonly value: string;
  readonly note: ReactNode;
  /** The command that uses the key, for a caller whose goal is a machine rather than a key. */
  readonly command?: string;
  readonly onDone: () => void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-4">
      <Banner
        icon={<CheckCircleIcon weight="fill" />}
        title="Copy the key now"
        description={note}
      />
      <ClipboardText
        size="base"
        text={value}
        tooltip={{ text: "Copy key", copiedText: "Copied" }}
        labels={{ copyAction: "Copy key" }}
      />
      {command === undefined ? null : (
        <div className="flex flex-col gap-1.5">
          <p className="text-kumo-subtle">
            Run this on the machine; it appears in the list within a few seconds.
          </p>
          <ClipboardText
            size="base"
            className="max-w-full"
            text={command}
            tooltip={{ text: "Copy command", copiedText: "Copied" }}
            labels={{ copyAction: "Copy command" }}
          />
        </div>
      )}
      <DialogFooter>
        <Button variant="primary" onClick={onDone}>
          Done
        </Button>
      </DialogFooter>
    </div>
  );
}
