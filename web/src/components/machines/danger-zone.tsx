import { Button } from "@cloudflare/kumo/components/button";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import type { Node } from "~/api/queries.ts";
import { DeleteDialog, ExpireDialog, SuspendDialog } from "~/components/machines/dialogs.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

type Pending = "suspend" | "expire" | "delete" | null;

/** The actions that cut a machine off, kept away from the rest of the page. */
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
          title={node.suspended ? "Lift the suspension" : "Suspend this machine"}
          description={
            node.suspended
              ? "Gives the machine its peers back. Nobody needs to sign in on it."
              : "Cuts the machine off without touching its key; you can lift it any time."
          }
          action={
            <Button
              variant="secondary"
              size="sm"
              loading={mutations.suspend.isPending}
              onClick={() => {
                if (node.suspended) {
                  mutations.suspend.mutate(
                    { params: { path: { nodeId: node.id } }, body: { suspended: false } },
                    {
                      onSuccess: () => {
                        toast.success("Suspension lifted");
                      },
                      onError: (error) => {
                        toast.error("Could not lift the suspension", error);
                      },
                    },
                  );
                } else {
                  setPending("suspend");
                }
              }}
            >
              {node.suspended ? "Lift suspension" : "Suspend"}
            </Button>
          }
        />
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
      <SuspendDialog
        node={node}
        mutations={mutations}
        open={pending === "suspend"}
        onOpenChange={close}
      />
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
