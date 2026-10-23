import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { DisabledReason } from "~/components/ui/disabled-reason.tsx";

/**
 * A key list's create action in its toolbar: a link to the create page, the dashboard's "Create
 * Token", or the same button switched off with the reason when the caller may not create.
 */
export function CreateKeyLink({
  to,
  label,
  allowed,
  variant = "primary",
}: {
  readonly to: string;
  readonly label: string;
  readonly allowed: boolean;
  readonly variant?: "primary" | "secondary";
}): ReactElement {
  if (allowed) {
    return (
      <LinkButton href={to} variant={variant} icon={PlusIcon}>
        {label}
      </LinkButton>
    );
  }

  return (
    <DisabledReason reason="Your credentials cannot create these">
      <Button variant={variant} icon={PlusIcon} disabled>
        {label}
      </Button>
    </DisabledReason>
  );
}
