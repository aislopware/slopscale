import { Button } from "@cloudflare/kumo/components/button";
import type { Icon } from "@phosphor-icons/react";
import { DesktopIcon, MoonIcon, SunIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Theme } from "~/lib/theme.ts";
import { theme, useTheme } from "~/lib/theme.ts";

const order: readonly Theme[] = ["system", "light", "dark"];
const icons: Record<Theme, Icon> = {
  system: DesktopIcon,
  light: SunIcon,
  dark: MoonIcon,
};
const labels: Record<Theme, string> = {
  system: "Theme: follows the system",
  light: "Theme: light",
  dark: "Theme: dark",
};

export function ThemeToggle(): ReactElement {
  const current = useTheme();
  const next = order[(order.indexOf(current) + 1) % order.length] ?? "system";

  return (
    <Button
      variant="ghost"
      shape="square"
      size="sm"
      icon={icons[current]}
      aria-label={labels[current]}
      title={labels[current]}
      onClick={() => {
        theme.set(next);
      }}
    />
  );
}
