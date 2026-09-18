import { cn } from "@cloudflare/kumo/utils";
import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { consoleAuthQuery } from "~/auth/me.ts";
import { Mark } from "~/components/layout/mark.tsx";
import { useDarkMode } from "~/lib/theme.ts";

/** What the operator calls this server, and the logos they gave it. */
export interface Brand {
  readonly title: string;
  readonly logoUrl: string;
  readonly logoDarkUrl: string;
}

/** Before the server has answered, the console is the product it was built as. */
const fallback: Brand = { title: "Slopscale", logoUrl: "", logoDarkUrl: "" };

/**
 * The name and logo the operator put on this server. They ride on the public sign-in description,
 * which the root route loads before the first render, so the name is right on the first paint
 * instead of flipping once a query settles.
 */
export function useBrand(): Brand {
  const { data } = useQuery(consoleAuthQuery);

  return data?.branding ?? fallback;
}

/**
 * The brand's mark: the operator's logo where they configured one, the product's own otherwise. The
 * two need different constraints and the caller gives both. The mark is square and is sized; a logo
 * is an unknown shape, so it is bound by height and left to take the width it needs.
 *
 * The dark logo is chosen from the theme in effect rather than from a media query, because the
 * console's own toggle can disagree with the browser's preference. The server repeats the light URL
 * when there is no dark variant, so this stays one image either way.
 */
export function BrandMark({
  markClassName,
  logoClassName,
}: {
  readonly markClassName: string;
  readonly logoClassName: string;
}): ReactElement {
  const brand = useBrand();
  const dark = useDarkMode();

  if (brand.logoUrl === "") {
    return <Mark className={markClassName} />;
  }

  return (
    <img
      src={dark && brand.logoDarkUrl !== "" ? brand.logoDarkUrl : brand.logoUrl}
      alt=""
      aria-hidden
      className={cn("w-auto object-contain object-left", logoClassName)}
    />
  );
}
