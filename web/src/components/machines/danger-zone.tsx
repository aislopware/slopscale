import { Button } from "@cloudflare/kumo/components/button";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import type { Node } from "~/api/queries.ts";
import { DeleteDialog, ExpireDialog } from "~/components/machines/dialogs.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { Section, SectionRow } from "~/components/ui/section.tsx";

type Pending = "expire" | "delete" | null;

/** The two actions an operator cannot take back, kept away from the rest of the page. */
export function DangerZone({ node }: { readonly node: Node }): ReactElement {
  const mutations = useNodeMutations();
  const [pending, setPending] = useState<Pending>(null);
  const close = (open: boolean): void => {
    if (!open) {
      setPending(null);
    }
  };

  return (
    <>
      <Section title="Danger zone">
        <DangerRow
          title="Expire the machine key"
          description="Disconnects the machine until someone signs in on it again."
          action={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                setPending("expire");
              }}
            >
              Expire key
            </Button>
          }
        />
        <DangerRow
          title="Remove this machine"
          description="Deletes it with its routes and sharing. The device can register again."
          action={
            <Button
              variant="destructive"
              size="sm"
              onClick={() => {
                setPending("delete");
              }}
            >
              Remove
            </Button>
          }
        />
      </Section>
      <ExpireDialog
        node={node}
        mutations={mutations}
        open={pending === "expire"}
        onOpenChange={close}
      />
      <DeleteDialog
        node={node}
        mutations={mutations}
        open={pending === "delete"}
        onOpenChange={close}
      />
    </>
  );
}

function DangerRow({
  title,
  description,
  action,
}: {
  readonly title: string;
  readonly description: string;
  readonly action: ReactNode;
}): ReactElement {
  return (
    <SectionRow className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 flex-col gap-1">
        <span className="font-medium text-kumo-strong">{title}</span>
        <span className="text-xs text-kumo-subtle">{description}</span>
      </div>
      {action}
    </SectionRow>
  );
}
