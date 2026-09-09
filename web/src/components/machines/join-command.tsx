import { Tabs } from "@cloudflare/kumo/components/tabs";
import { useState } from "react";
import type { ReactElement } from "react";

import {
  isPlatform,
  joinInstructions,
  platformLabels,
  platforms,
} from "~/components/machines/connect.ts";
import type { Platform } from "~/components/machines/connect.ts";
import { CommandBox } from "~/components/ui/command-text.tsx";
import { QrCode } from "~/components/ui/qr-code.tsx";

const tabs = platforms.map((platform) => ({ value: platform, label: platformLabels[platform] }));

/**
 * The command that joins a machine with the key, one tab per platform, next to a QR code that
 * carries the same line so a phone or a machine without a shared clipboard can pick it up.
 */
export function JoinCommand({ authKey }: { readonly authKey: string }): ReactElement {
  const [platform, setPlatform] = useState<Platform>("linux");
  const instructions = joinInstructions(platform, authKey);

  return (
    <div className="flex flex-col gap-3">
      <Tabs
        variant="segmented"
        tabs={tabs}
        value={platform}
        onValueChange={(value) => {
          setPlatform(isPlatform(value) ? value : "linux");
        }}
      />
      <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
        <div className="flex min-w-0 flex-col gap-1.5">
          {instructions.command === null ? null : (
            <CommandBox command={instructions.command} wrap />
          )}
          <p className="text-kumo-subtle">{instructions.note}</p>
          {instructions.command === null ? null : (
            <p className="text-kumo-subtle">
              It appears in the machine list within a few seconds of running.
            </p>
          )}
        </div>
        <QrCode
          text={instructions.qr}
          label={
            instructions.command === null
              ? "QR code with the server address"
              : `QR code with the ${platformLabels[platform]} join command`
          }
          className="size-32 shrink-0 justify-self-center rounded-md"
        />
      </div>
    </div>
  );
}
