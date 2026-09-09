import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { DotsThreeIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

const triggerIconSize = 18;

/**
 * The "…" at the end of a row, and the menu it opens. Every list and table offers its actions
 * through one of these, never as a run of icon buttons: one control per row is quieter, the words
 * say what each action does, and a destructive one sits behind a separator.
 */
export function RowMenu({
  label,
  disabled = false,
  children,
}: {
  /** Accessible name of the trigger, such as "Actions for region fra". */
  readonly label: string;
  readonly disabled?: boolean;
  /** The `DropdownMenu.Item`s and separators. */
  readonly children: ReactNode;
}): ReactElement {
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            icon={<DotsThreeIcon size={triggerIconSize} weight="bold" />}
            aria-label={label}
            disabled={disabled}
          />
        }
      />
      <DropdownMenu.Content align="end">{children}</DropdownMenu.Content>
    </DropdownMenu>
  );
}
