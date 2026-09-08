import type { LinkComponentProps } from "@cloudflare/kumo/utils";
import { Link } from "@tanstack/react-router";
import { forwardRef } from "react";
import type { ReactElement, Ref } from "react";

function RouterLink(
  // oxlint-disable-next-line typescript/no-deprecated -- Breadcrumbs.Link still sends `to`
  { href, to, target, ...rest }: LinkComponentProps,
  ref: Ref<HTMLAnchorElement>,
): ReactElement {
  return (
    <Link
      {...rest}
      ref={ref}
      to={href ?? to ?? "/"}
      {...(target === undefined ? {} : { target })}
    />
  );
}

/**
 * Bridges Kumo's `href`-based links (Link, LinkButton, Sidebar.MenuButton, DropdownMenu.LinkItem)
 * to the router so they navigate client-side and preload on intent. Kumo requires a forwardRef
 * component here. Breadcrumbs.Link still hands its address over as the deprecated `to`, so both are
 * read, or every breadcrumb would lead to the overview.
 */
export const AppLink = forwardRef(RouterLink);
