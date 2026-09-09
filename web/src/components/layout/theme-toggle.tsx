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

const iconSize = 16;

/**
 * The three states the theme can be in, named. A cycling button asked the operator to guess. The
 * trigger is an outlined button the height of the avatar beside it, with the state as its label, so
 * the top bar's two controls read as a pair.
 */
export function ThemeToggle(): ReactElement {
  const current = useTheme();
  const active = options.find((option) => option.value === current) ?? options.at(-1);
  const Icon = active?.icon ?? DesktopIcon;

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button
            variant="outline"
            className="h-8 gap-1.5 px-2.5 text-sm max-sm:px-2"
            aria-label={labels[current]}
          >
            <Icon size={iconSize} aria-hidden />
            {/* Small screens keep the icon alone; the aria-label still names the state. */}
            <span className="max-sm:hidden">{active?.label}</span>
          </Button>
        }
      />
      <DropdownMenu.Content align="end" className="min-w-40">
        <DropdownMenu.Group>
          <DropdownMenu.Label>Theme</DropdownMenu.Label>
          {options.map((option) => (
            <DropdownMenu.Item
              key={option.value}
              icon={option.icon}
              selected={option.value === current}
              onClick={() => {
                theme.set(option.value);
              }}
            >
              <span className="flex-1 pr-3">{option.label}</span>
            </DropdownMenu.Item>
          ))}
        </DropdownMenu.Group>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
