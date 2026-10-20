import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import type { Icon } from "@phosphor-icons/react";
import { DesktopIcon, MoonIcon, SunIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Theme } from "~/lib/theme.ts";
import { theme, useTheme } from "~/lib/theme.ts";

const options: readonly { readonly value: Theme; readonly label: string; readonly icon: Icon }[] = [
  { value: "light", label: "Light", icon: SunIcon },
  { value: "dark", label: "Dark", icon: MoonIcon },
  { value: "system", label: "System", icon: DesktopIcon },
];

const labels: Record<Theme, string> = {
  system: "System theme",
  light: "Light theme",
  dark: "Dark theme",
};

function isTheme(value: unknown): value is Theme {
  return options.some((option) => option.value === value);
}

/** The three themes as radio rows, shared by the submenu and the stand-alone menu. */
function ThemeOptions(): ReactElement {
  const current = useTheme();

  return (
    <DropdownMenu.RadioGroup
      value={current}
      onValueChange={(value: unknown) => {
        if (isTheme(value)) {
          theme.set(value);
        }
      }}
    >
      {options.map((option) => (
        <DropdownMenu.RadioItem key={option.value} value={option.value} icon={option.icon}>
          <span className="flex-1 pr-3">{option.label}</span>
          <DropdownMenu.RadioItemIndicator />
        </DropdownMenu.RadioItem>
      ))}
    </DropdownMenu.RadioGroup>
  );
}

/**
 * "Appearance" in the user menu, as the Cloudflare dashboard has it: a submenu row whose options
 * are the three themes, the current one checked.
 */
export function AppearanceSubmenu(): ReactElement {
  return (
    <DropdownMenu.Sub>
      <DropdownMenu.SubTrigger>Appearance</DropdownMenu.SubTrigger>
      <DropdownMenu.SubContent className="min-w-36">
        <ThemeOptions />
      </DropdownMenu.SubContent>
    </DropdownMenu.Sub>
  );
}

/**
 * The theme on its own, for the pages outside the shell (sign-in, a page that failed) that have no
 * user menu: a 32px ghost icon button like the top bar's, opening the same three options.
 */
export function ThemeToggle(): ReactElement {
  const current = useTheme();
  const CurrentIcon = options.find((option) => option.value === current)?.icon ?? DesktopIcon;

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="ghost" shape="square" className="size-8" aria-label={labels[current]}>
            <CurrentIcon aria-hidden className="size-4 text-kumo-subtle" />
          </Button>
        }
      />
      <DropdownMenu.Content align="end" className="min-w-36">
        <DropdownMenu.Group>
          <DropdownMenu.Label>Appearance</DropdownMenu.Label>
          <ThemeOptions />
        </DropdownMenu.Group>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
