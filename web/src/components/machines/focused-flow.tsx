import { LinkButton } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { XIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useEffect } from "react";
import type { ReactElement, ReactNode } from "react";

import { BrandMark, useBrand } from "~/components/layout/brand.tsx";
import { documentTitle } from "~/components/layout/shell.tsx";

/** One entry of the stepper on the right. */
export interface FlowStep {
  readonly id: string;
  readonly label: string;
}

/**
 * The 620px column between the guides: the 560px step cards and 30px either side. The page and its
 * guide overlay share the template, so the dashed lines run exactly along the column's edges.
 */
const columnsClass = "md:grid-cols-[minmax(0,1fr)_minmax(0,620px)_minmax(0,1fr)]";

/**
 * The full-screen frame of a focused flow, as Cloudflare draws Mesh's "Add node": no sidebar, a bar
 * with the brand, the flow's name and a close button, blueprint guides (dashed lines with small
 * squares where they cross) framing the column of step cards, and a dot stepper to the right of the
 * column. On a phone the guides go and the stepper sits above the cards.
 */
export function FocusedFlow({
  title,
  section,
  closeHref,
  steps,
  current,
  children,
}: {
  readonly title: string;
  /** The sidebar page the flow belongs to, for the tab's title: "Machines". */
  readonly section: string;
  /** Where the close button goes: the page the flow was opened from. */
  readonly closeHref: string;
  readonly steps?: readonly FlowStep[];
  readonly current?: string;
  readonly children: ReactNode;
}): ReactElement {
  const brand = useBrand();
  const tabTitle = documentTitle(brand.title, section, title);

  useEffect(() => {
    document.title = tabTitle;
  }, [tabTitle]);

  return (
    <div className="relative isolate min-h-svh bg-kumo-canvas">
      <div
        aria-hidden
        className={cn(
          "pointer-events-none absolute inset-0 -z-1 max-md:hidden md:grid",
          columnsClass,
        )}
      >
        <div className="col-start-2 border-x border-dashed border-kumo-line" />
      </div>
      <header className="flex h-14 items-center justify-between gap-4 px-4 md:px-7">
        <div className="flex min-w-0 items-center gap-4">
          <Link
            to="/"
            className="flex min-w-0 items-center gap-2.5 rounded-lg py-1 text-kumo-default no-underline outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
          >
            <BrandMark markClassName="size-6 shrink-0" logoClassName="h-6 max-w-24 shrink-0" />
            <span className="truncate text-sm font-medium max-sm:hidden">{brand.title}</span>
          </Link>
          <h1 className="truncate text-base font-medium text-kumo-default">{title}</h1>
        </div>
        <LinkButton
          href={closeHref}
          variant="ghost"
          shape="square"
          aria-label={`Close ${title.toLowerCase()}`}
          icon={<XIcon aria-hidden className="size-5" />}
        />
      </header>
      <div
        className={cn(
          "relative mt-4 grid border-kumo-line md:my-16 md:border-y md:border-dashed",
          columnsClass,
        )}
      >
        <GuideCorners />
        {steps === undefined ? null : (
          <ol
            aria-label="Steps"
            className="flex flex-wrap gap-x-4 gap-y-2 px-4 pb-4 md:col-start-3 md:row-start-1 md:flex-col md:gap-3 md:px-6 md:py-[46px]"
          >
            {steps.map((item) => (
              <li
                key={item.id}
                aria-current={item.id === current ? "step" : undefined}
                className={cn(
                  "flex items-center gap-2.5 text-sm",
                  item.id === current ? "font-medium text-kumo-default" : "text-kumo-subtle",
                )}
              >
                <span
                  aria-hidden
                  className={cn(
                    "size-1.5 rounded-full ring ring-current",
                    item.id === current && "bg-current",
                  )}
                />
                {item.label}
              </li>
            ))}
          </ol>
        )}
        <main className="flex min-w-0 flex-col gap-4 px-4 pb-8 md:col-start-2 md:row-start-1 md:p-7.5">
          {children}
        </main>
      </div>
    </div>
  );
}

/** The small squares where the dashed guides cross, as on Cloudflare's blueprint frame. */
function GuideCorners(): ReactElement {
  const square = "absolute size-[7px] border border-kumo-line bg-kumo-canvas";

  return (
    <div
      aria-hidden
      className={cn("pointer-events-none absolute inset-0 max-md:hidden md:grid", columnsClass)}
    >
      <div className="relative col-start-2">
        <span className={cn(square, "-top-1 -left-1")} />
        <span className={cn(square, "-top-1 -right-1")} />
        <span className={cn(square, "-bottom-1 -left-1")} />
        <span className={cn(square, "-right-1 -bottom-1")} />
      </div>
    </div>
  );
}
