import { Badge } from "@cloudflare/kumo/components/badge";
import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { docsUrl } from "~/lib/docs.ts";

/**
 * "View docs": Kumo's outline Badge as a link to a page of the slopscale documentation, the pill
 * the dashboard puts beside a page title or a setting's name. Kumo rings a badge in the text colour
 * while the link around it is hovered.
 */
export function DocsBadge({
  path,
  label = "View docs",
}: {
  /** The page under `docs/content`, without the extension: "ref/dns". */
  readonly path: string;
  readonly label?: string;
}): ReactElement {
  return (
    <a
      href={docsUrl(path)}
      target="_blank"
      rel="noreferrer"
      className="inline-flex shrink-0 rounded-full no-underline outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
    >
      <Badge variant="outline" icon={<ArrowSquareOutIcon aria-hidden />}>
        {label}
      </Badge>
    </a>
  );
}
