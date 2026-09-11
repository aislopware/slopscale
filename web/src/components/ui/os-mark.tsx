import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import {
  AndroidLogoIcon,
  AppleLogoIcon,
  DesktopIcon,
  DeviceMobileIcon,
  LinuxLogoIcon,
  TelevisionIcon,
  WindowsLogoIcon,
} from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { osFamily, osLabel } from "~/lib/os.ts";
import type { OsFamily } from "~/lib/os.ts";

const marks: Record<OsFamily, Icon> = {
  macos: AppleLogoIcon,
  ios: DeviceMobileIcon,
  tvos: TelevisionIcon,
  windows: WindowsLogoIcon,
  linux: LinuxLogoIcon,
  android: AndroidLogoIcon,
  freebsd: DesktopIcon,
  other: DesktopIcon,
};

/**
 * The operating system a machine runs, as its logo: a column of machines is read by the mark before
 * the name, the way a laptop and a phone tell apart on a desk. The mark is in the subtle text
 * colour so it labels rather than decorates, and the full name is one hover away; a machine that
 * has not connected yet draws no mark, since the console does not know.
 */
export function OsMark({
  os,
  version = "",
  size = 14,
  className,
}: {
  readonly os: string;
  readonly version?: string;
  readonly size?: number;
  readonly className?: string;
}): ReactElement | null {
  if (os.trim() === "") {
    return null;
  }

  const Mark = marks[osFamily(os)];

  return (
    <span
      title={osLabel(os, version)}
      className={cn("inline-flex h-lh shrink-0 items-center text-kumo-subtle", className)}
    >
      <Mark size={size} weight="fill" aria-label={osLabel(os, version)} />
    </span>
  );
}
