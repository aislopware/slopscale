import type { LinkComponentProps } from "@cloudflare/kumo/utils";
import { Link } from "@tanstack/react-router";
import { forwardRef } from "react";
import type { ReactElement, Ref } from "react";

function RouterLink(
  { href, target, ...rest }: Omit<LinkComponentProps, "to">,
  ref: Ref<HTMLAnchorElement>,
): ReactElement {
  return (
    <Link {...rest} ref={ref} to={href ?? "/"} {...(target === undefined ? {} : { target })} />
  );
}

/**
 * Bridges Kumo's `href`-based links (Link, LinkButton, Sidebar.MenuButton, DropdownMenu.LinkItem)
 * to the router so they navigate client-side and preload on intent. Kumo requires a forwardRef
 * component here.
 */
export const AppLink = forwardRef(RouterLink);
