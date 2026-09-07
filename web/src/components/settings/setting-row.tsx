import type { ReactElement, ReactNode } from "react";

import { SectionRow } from "~/components/ui/section.tsx";

/** Title and description on the left, the control on the right; hairlines between rows. */
export function SettingRow({
  title,
  description,
  control,
}: {
  readonly title: string;
  readonly description: ReactNode;
  readonly control: ReactElement;
}): ReactElement {
  return (
    <SectionRow className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1">
        <span className="font-medium text-kumo-strong">{title}</span>
        <p className="max-w-prose text-kumo-subtle">{description}</p>
      </div>
      <span className="flex h-lh shrink-0 items-center">{control}</span>
    </SectionRow>
  );
}
