import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowsClockwiseIcon, PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { usePostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import {
  DeletePostureIntegrationDialog,
  PostureIntegrationDialog,
} from "~/components/posture-integrations/posture-integration-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { toast } from "~/components/ui/toast.ts";

type Dialog = "edit" | "delete";

export function PostureIntegrationMenu({
  integration,
  providers,
  me,
}: {
  readonly integration: PostureIntegration;
  readonly providers: readonly PostureProvider[];
  readonly me: Me;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = usePostureIntegrationMutations();
  const writable = can(me, "devices:posture_attributes");

  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  const syncNow = (): void => {
    mutations.sync.mutate(
      { params: { path: { id: integration.id } } },
      {
        onError: (error) => {
          toast.error(errorMessage(error));
        },
      },
    );
  };

  return (
    <>
      <RowMenu label={`Actions for ${integration.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          disabled={!writable}
          onClick={() => {
            setDialog("edit");
          }}
        >
          Edit
        </DropdownMenu.Item>
        <DropdownMenu.Item
          icon={ArrowsClockwiseIcon}
          disabled={!writable || mutations.sync.isPending}
          onClick={syncNow}
        >
          Sync now
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
          Delete
        </DropdownMenu.Item>
      </RowMenu>
      <PostureIntegrationDialog
        integration={integration}
        providers={providers}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeletePostureIntegrationDialog
        integration={integration}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
