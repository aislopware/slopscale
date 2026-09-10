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
 * The only empty state that can offer something: there is not a single user yet. The toolbar above
 * already holds the page's primary "Add user", so this one is secondary.
 */
export function FirstUserEmpty({ me }: { readonly me: Me }): ReactElement {
  const [open, setOpen] = useState(false);
  const mutations = useUserMutations();

  return (
    <>
      <Empty
        className={tableEmptyClass}
        size="sm"
        title="No users yet"
        description="Users appear here after their first sign-in. Add one now to hand out a pre-auth key."
        contents={
          <Button
            variant="secondary"
            disabled={!can(me, "users")}
            onClick={() => {
              setOpen(true);
            }}
          >
            Add user
          </Button>
        }
      />
      <CreateUserDialog open={open} onOpenChange={setOpen} mutations={mutations} />
    </>
  );
}
