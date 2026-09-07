import { Button } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { CreateUserDialog } from "~/components/users/dialogs.tsx";
import { useUserMutations } from "~/components/users/mutations.ts";

/** The page header's create action; the form lives inside the dialog, so it starts empty every time. */
export function AddUserButton({ me }: { readonly me: Me }): ReactElement {
  const [open, setOpen] = useState(false);
  const mutations = useUserMutations();

  return (
    <>
      <Button
        variant="primary"
        icon={PlusIcon}
        disabled={!can(me, "users")}
        onClick={() => {
          setOpen(true);
        }}
      >
        Add user
      </Button>
      <CreateUserDialog open={open} onOpenChange={setOpen} mutations={mutations} />
    </>
  );
}
