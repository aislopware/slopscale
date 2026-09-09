import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  DotsThreeIcon,
  PaperPlaneTiltIcon,
  PencilSimpleIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { LogStream } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { DeleteLogStreamDialog, LogStreamDialog } from "~/components/logstreams/dialogs.tsx";
import { useLogStreamMutations } from "~/components/logstreams/mutations.ts";
import { toast } from "~/components/ui/toast.ts";

const actionsIconSize = 18;

type Dialog = "edit" | "delete";

/** Test, edit and delete for one log stream, gated by the logs scope. */
export function LogStreamMenu({
  stream,
  me,
}: {
  readonly stream: LogStream;
  readonly me: Me;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useLogStreamMutations();
  const writable = can(me, "logs:configuration");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };
  const sendTest = (): void => {
    mutations.test.mutate(
      { params: { path: { id: stream.id } } },
      {
        onSuccess: (data) => {
          if (data.delivered) {
            toast.success(`Test entry delivered (HTTP ${data.status})`);
          } else {
            toast.error(`Test entry failed (${data.status})`);
          }
        },
        onError: (error) => {
          toast.error(errorMessage(error));
        },
      },
    );
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              variant="ghost"
              shape="square"
              size="sm"
              icon={<DotsThreeIcon size={actionsIconSize} weight="bold" />}
              aria-label={`Actions for log stream ${stream.name}`}
            />
          }
        />
        <DropdownMenu.Content align="end">
          <DropdownMenu.Item
            icon={PaperPlaneTiltIcon}
            disabled={!writable || mutations.test.isPending}
            onClick={sendTest}
          >
            Send test entry
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={PencilSimpleIcon}
            disabled={!writable}
            onClick={() => {
              setDialog("edit");
            }}
          >
            Edit…
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            disabled={!writable}
            onClick={() => {
              setDialog("delete");
            }}
          >
            Delete…
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
      <LogStreamDialog
        stream={stream}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeleteLogStreamDialog
        stream={stream}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
