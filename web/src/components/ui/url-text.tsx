import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { urlParts } from "~/lib/url-parts.ts";
import type { UrlPart } from "~/lib/url-parts.ts";

/** The host carries the meaning of a URL; the scheme, port, path and query step back. */
const partClasses: Readonly<Record<UrlPart["kind"], string>> = {
  scheme: "text-kumo-subtle",
  host: "text-kumo-default",
  port: "text-kumo-subtle",
  path: "text-kumo-subtle",
  query: "text-kumo-subtle",
  hash: "text-kumo-subtle",
  text: "",
};

/** A URL with its host in the foreground, monospace, breaking anywhere when it must wrap. */
export function UrlText({
  url,
  className,
}: {
  readonly url: string;
  readonly className?: string;
}): ReactElement {
  return (
    <span className={cn("font-mono", className)}>
      {urlParts(url).map((part, index) => (
        // Parts repeat their text ("/" twice) but never their position.
        // eslint-disable-next-line react/no-array-index-key
        <span key={index} className={partClasses[part.kind]}>
          {part.text}
        </span>
      ))}
    </span>
  );
}
