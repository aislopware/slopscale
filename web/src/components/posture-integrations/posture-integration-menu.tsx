import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowsClockwiseIcon, PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PostureIntegration } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { usePostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import type { PostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import { DeletePostureIntegrationDialog } from "~/components/posture-integrations/posture-integration-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Starts a sync now; the mutation says so in a toast, and a refusal says why. */
function syncNow(mutations: PostureIntegrationMutations, integration: PostureIntegration): void {
  mutations.sync.mutate(
    { params: { path: { id: integration.id } } },
    {
      onError: (error) => {
        toast.error(errorMessage(error));
      },
    },
  );
}

/** Edit, sync and delete for one integration, gated by the posture scope. Editing opens its page. */
export function PostureIntegrationMenu({
  integration,
  me,
}: {
  readonly integration: PostureIntegration;
  readonly me: Me;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = usePostureIntegrationMutations();
  const writable = can(me, "devices:posture_attributes");

  return (
    <>
      <RowMenu label={`Actions for ${integration.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({
              to: "/integrations/posture/$integrationId",
              params: { integrationId: integration.id },
            });
          }}
        >
          {writable ? "Edit" : "View"}
        </DropdownMenu.Item>
        <DropdownMenu.Item
          icon={ArrowsClockwiseIcon}
          disabled={!writable || mutations.sync.isPending}
          onClick={() => {
            syncNow(mutations, integration);
          }}
        >
          Sync now
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
      <DeletePostureIntegrationDialog
        integration={integration}
        open={deleting}
        onOpenChange={setDeleting}
        mutations={mutations}
      />
    </>
  );
}

/** The integration page's own action beside its title: sync the saved settings now. */
export function SyncNowButton({
  integration,
  canEdit,
}: {
  readonly integration: PostureIntegration;
  readonly canEdit: boolean;
}): ReactElement {
  const mutations = usePostureIntegrationMutations();

  return (
    <Button
      variant="secondary"
      icon={ArrowsClockwiseIcon}
      disabled={!canEdit}
      loading={mutations.sync.isPending}
      onClick={() => {
        syncNow(mutations, integration);
      }}
    >
      Sync now
    </Button>
  );
}
