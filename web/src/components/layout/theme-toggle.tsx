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
  system: "Theme: system",
  light: "Theme: light",
  dark: "Theme: dark",
};

/** The three states the theme can be in, named. A cycling button asked the operator to guess. */
export function ThemeToggle(): ReactElement {
  const current = useTheme();
  const active = options.find((option) => option.value === current) ?? options.at(-1);

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            icon={active?.icon}
            aria-label={labels[current]}
          />
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
