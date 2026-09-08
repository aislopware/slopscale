import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

/** The "nothing here" panel, shared by the in-app page and the bare fallback outside it. */
export function NotFoundPanel(): ReactElement {
  return (
    <Empty
      title="Page not found"
      description="Nothing lives at this address."
      contents={
        <Link to="/">
          <Button variant="secondary">Back to overview</Button>
        </Link>
      }
    />
  );
}
