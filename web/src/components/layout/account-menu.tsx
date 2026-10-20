import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { UserIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Me } from "~/auth/me.ts";
import { displayName, roleLabel } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import { AppearanceSubmenu } from "~/components/layout/theme-toggle.tsx";

const kindLabels: Record<string, string> = {
  local: "Local socket",
  api_key: "API key",
  oauth: "OAuth token",
  session: "Browser session",
};

/**
 * The user menu: a 32px ghost button with a user icon (no avatar, as on the dashboard) opening who
 * is signed in, the appearance submenu, and signing out in the danger colour.
 */
export function AccountMenu({ me }: { readonly me: Me }): ReactElement {
  const name = displayName(me);
  const role = roleLabel(me);
  const kind = kindLabels[me.kind] ?? me.kind;

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button
            variant="ghost"
            shape="square"
            className="size-8"
            aria-label="Account"
            title={name}
          >
            <UserIcon aria-hidden weight="fill" className="size-4 text-kumo-subtle" />
          </Button>
        }
      />
      <DropdownMenu.Content align="end" className="min-w-56">
        <DropdownMenu.Group>
          <DropdownMenu.Label className="flex flex-col gap-0.5 py-1 text-xs font-normal">
            <span className="truncate text-kumo-default">{name}</span>
            <span className="flex min-w-0 items-center gap-1 text-kumo-subtle">
              <span className="truncate">{kind}</span>
              {role === null ? null : <span className="truncate">{`· ${role}`}</span>}
            </span>
          </DropdownMenu.Label>
        </DropdownMenu.Group>
        <DropdownMenu.Separator />
        <AppearanceSubmenu />
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          variant="danger"
          onClick={() => {
            void signOut();
          }}
        >
          Sign out
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
