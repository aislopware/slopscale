import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { tableEmptyClass } from "~/components/table/empty.ts";
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

/**
 * There is not a single user yet. It says where users come from and stops there: the toolbar an
 * inch above holds "Add user", and a second copy of it inside the empty panel only made the page
 * ask twice.
 */
export function FirstUserEmpty(): ReactElement {
  return (
    <Empty
      className={tableEmptyClass}
      size="sm"
      title="No users yet"
      description="Users appear here after their first sign-in, or add one to hand out a pre-auth key."
    />
  );
}
