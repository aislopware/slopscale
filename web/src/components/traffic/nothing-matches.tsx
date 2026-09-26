import { Button } from "@cloudflare/kumo/components/button";
import type { ReactElement } from "react";

import { SectionEmpty } from "~/components/ui/section.tsx";

/** The empty table of a filtered traffic list, with the way back to everything. */
export function NothingMatches({
  description,
  onClear,
}: {
  readonly description: string;
  readonly onClear: () => void;
}): ReactElement {
  return (
    <SectionEmpty
      title="Nothing matches"
      description={description}
      contents={
        <Button variant="secondary" onClick={onClear}>
          Clear filters
        </Button>
      }
    />
  );
}
