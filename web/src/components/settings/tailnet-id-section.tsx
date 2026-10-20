import type { ReactElement } from "react";

import { CopyButton } from "~/components/ui/copy-text.tsx";
import { SettingsCard, SettingsRow } from "~/components/ui/settings-card.tsx";
import { SettingsGroup } from "~/components/ui/settings-layout.tsx";

const monoClass = "font-mono text-[0.9em]";

/** The tailnet's stable ID, which the Tailscale-compatible API takes in place of `-`. */
export function TailnetIDSection({ tailnetId }: { readonly tailnetId: string }): ReactElement {
  return (
    <SettingsGroup id="general" title="General">
      <SettingsCard>
        <SettingsRow
          title="Tailnet ID"
          docs="ref/api"
          description={
            <>
              {"Use the tailnet ID or "}
              <span className={monoClass}>-</span>
              {" wherever an "}
              <span className={monoClass}>/api/v2</span>
              {" path names the tailnet. Machines report it as "}
              <span className={monoClass}>CurrentTailnet.StableID</span>
              {" in "}
              <span className={monoClass}>tailscale status --json</span>.
            </>
          }
        >
          {/* The ID with a copy button that is always there, the way Zero Trust's settings show
              an ID; it wraps rather than cut, since a phone is narrower than the ID. */}
          <span className="flex min-w-0 items-center gap-1">
            <span className="min-w-0 font-mono text-[0.9em] break-all">{tailnetId}</span>
            <CopyButton value={tailnetId} label="Copy tailnet ID" />
          </span>
        </SettingsRow>
      </SettingsCard>
    </SettingsGroup>
  );
}
