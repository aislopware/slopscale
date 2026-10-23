import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PaperPlaneTiltIcon, PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { LogStream } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { DeleteLogStreamDialog } from "~/components/logstreams/dialogs.tsx";
import { useLogStreamMutations } from "~/components/logstreams/mutations.ts";
import type { LogStreamMutations } from "~/components/logstreams/mutations.ts";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Ships the stream one test entry and says in a toast how the destination answered. */
function sendTest(mutations: LogStreamMutations, stream: LogStream): void {
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
}

/** Test, edit and delete for one log stream, gated by the logs scope. Editing opens its page. */
export function LogStreamMenu({
  stream,
  me,
}: {
  readonly stream: LogStream;
  readonly me: Me;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = useLogStreamMutations();
  const writable = can(me, "logs:configuration");

  return (
    <>
      <RowMenu label={`Actions for log stream ${stream.name}`}>
        <DropdownMenu.Item
          icon={PaperPlaneTiltIcon}
          disabled={!writable || mutations.test.isPending}
          onClick={() => {
            sendTest(mutations, stream);
          }}
        >
          Send test entry
        </DropdownMenu.Item>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({
              to: "/integrations/log-streams/$streamId",
              params: { streamId: stream.id },
            });
          }}
        >
          {writable ? "Edit" : "View"}
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={TrashIcon}
          variant="danger"
          disabled={!writable}
          onClick={() => {
            setDeleting(true);
          }}
        >
          Delete…
        </DropdownMenu.Item>
      </RowMenu>
      <DeleteLogStreamDialog
        stream={stream}
        open={deleting}
        onOpenChange={setDeleting}
        mutations={mutations}
      />
    </>
  );
}

/** The stream page's own action beside its title: ship a test entry with the saved settings. */
export function SendTestEntryButton({
  stream,
  canEdit,
}: {
  readonly stream: LogStream;
  readonly canEdit: boolean;
}): ReactElement {
  const mutations = useLogStreamMutations();

  return (
    <Button
      variant="secondary"
      icon={PaperPlaneTiltIcon}
      disabled={!canEdit}
      loading={mutations.test.isPending}
      onClick={() => {
        sendTest(mutations, stream);
      }}
    >
      Send test entry
    </Button>
  );
}
