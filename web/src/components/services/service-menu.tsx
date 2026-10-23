import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Service } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { serviceLabel } from "~/components/services/model.ts";
import { useServiceMutations } from "~/components/services/mutations.ts";
import { DeleteServiceDialog } from "~/components/services/service-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/** Edit and delete for one service, gated by the services scope. Editing opens its edit page. */
export function ServiceMenu({
  service,
  me,
  onDeleted,
}: {
  readonly service: Service;
  readonly me: Me;
  /** Called once the service is gone, so a detail page can leave. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = useServiceMutations();
  const writable = can(me, "services");

  return (
    <>
      <RowMenu label={`Actions for service ${serviceLabel(service.name)}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          disabled={!writable}
          onClick={() => {
            void navigate({
              to: "/services/$label/edit",
              params: { label: serviceLabel(service.name) },
            });
          }}
        >
          Edit
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
      <DeleteServiceDialog
        service={service}
        open={deleting}
        onOpenChange={setDeleting}
        mutations={mutations}
        {...(onDeleted === undefined ? {} : { onDeleted })}
      />
    </>
  );
}
