import type { ReactElement, ReactNode } from "react";

import { SettingsRow } from "~/components/ui/settings-card.tsx";

/** A setting inside a Section panel: the dashboard's settings row, its control on the right. */
export function SettingRow({
  title,
  description,
  control,
}: {
  readonly title: string;
  readonly description: ReactNode;
  readonly control: ReactElement;
}): ReactElement {
  return <SettingsRow title={title} description={description} control={control} />;
}
