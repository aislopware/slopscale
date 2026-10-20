import type { ReactElement } from "react";

import { CopyText } from "~/components/ui/copy-text.tsx";
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
          <CopyText value={tailnetId} className="w-fit" />
        </SettingsRow>
      </SettingsCard>
    </SettingsGroup>
  );
}
