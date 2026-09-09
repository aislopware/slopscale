import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Service } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { serviceLabel } from "~/components/services/model.ts";
import { useServiceMutations } from "~/components/services/mutations.ts";
import { DeleteServiceDialog, ServiceDialog } from "~/components/services/service-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

type Dialog = "edit" | "delete";

/** Edit and delete for one service, gated by the services scope. */
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
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useServiceMutations();
  const writable = can(me, "services");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  return (
    <>
      <RowMenu label={`Actions for service ${serviceLabel(service.name)}`}>
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
      </RowMenu>
      <ServiceDialog
        service={service}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeleteServiceDialog
        service={service}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
        {...(onDeleted === undefined ? {} : { onDeleted })}
      />
    </>
  );
}
