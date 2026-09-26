import type { ReactElement } from "react";

import { DefinitionList } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";

const monoClass = "font-mono text-[0.9em]";

/** The tailnet's stable ID, which the Tailscale-compatible API takes in place of `-`. */
export function TailnetIDSection({ tailnetId }: { readonly tailnetId: string }): ReactElement {
  return (
    <Section
      title="General"
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
      bodyClassName="p-0"
    >
      <DefinitionList
        items={[
          {
            label: "Tailnet ID",
            value: tailnetId,
            copy: tailnetId,
          },
        ]}
      />
    </Section>
  );
}
