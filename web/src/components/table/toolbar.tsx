import { cn } from "@cloudflare/kumo/utils";
import { useEffect, useState } from "react";
import type { ReactElement, ReactNode } from "react";

/**
 * The controls above a list, as the dashboard's Workers and Security rules pages have them: search
 * and filters on the left (join them into one ringed group with Kumo's `Toolbar` and `SearchInput
 * joined`), the list's primary action on the right. The row sticks under the top bar while the list
 * scrolls, and once it is stuck it gains the elevated frame the dashboard's toolbar animates in, so
 * it reads as lifted off the rows sliding under it. It belongs with the list under it, so it pulls
 * the next block up from the page's 28px to 16px.
 */
export function TableToolbar({
  children,
  actions,
  className,
}: {
  readonly children: ReactNode;
  readonly actions?: ReactNode;
  readonly className?: string;
}): ReactElement {
  const [bar, setBar] = useState<HTMLDivElement | null>(null);
  const stuck = useStuck(bar);

  return (
    <div
      ref={setBar}
      data-stuck={stuck ? "" : undefined}
      className={cn(
        // A pixel under the top bar's edge, so a stuck bar is one pixel out of view and the observer
        // can tell it from one merely passing by. The shell sets the bar's height; outside it (a
        // test, say) there is no bar, and a 57px top would push a toolbar at the page's top down
        // over the rows. On a phone the controls wrap onto several lines, too tall to keep on screen.
        "z-5 -mx-2 -mb-3 flex flex-wrap items-start gap-2 rounded-xl p-2 ring ring-transparent transition-[background-color,box-shadow] duration-150 md:sticky md:top-[calc(var(--app-header-height,0px)-1px)]",
        "md:data-stuck:bg-kumo-elevated md:data-stuck:ring-kumo-hairline",
        className,
      )}
    >
      <div className="flex min-w-0 flex-1 basis-64 flex-wrap items-center gap-2">{children}</div>
      {actions === undefined ? null : (
        <div className="flex min-h-9 shrink-0 items-center gap-2">{actions}</div>
      )}
    </div>
  );
}

/** Whether a sticky toolbar is resting against the top bar. */
function useStuck(element: Element | null): boolean {
  const [stuck, setStuck] = useState(false);

  useEffect((): (() => void) | undefined => {
    if (element === null) {
      return undefined;
    }

    const headerHeight =
      Number(getComputedStyle(element).getPropertyValue("--app-header-height").replace("px", "")) ||
      0;
    const observer = new IntersectionObserver(
      ([entry]) => {
        setStuck(
          entry !== undefined &&
            entry.intersectionRatio < 1 &&
            entry.boundingClientRect.top <= headerHeight,
        );
      },
      { rootMargin: `-${headerHeight}px 0px 0px 0px`, threshold: [1] },
    );

    observer.observe(element);

    return (): void => {
      observer.disconnect();
    };
  }, [element]);

  return stuck;
}

/**
 * "Showing 3 of 12" under a table card, in 13px subtle figures, with room for controls on the
 * right.
 */
export function TableFooter({
  children,
  actions,
}: {
  readonly children: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <div className="flex items-center justify-between gap-3 text-sm text-kumo-subtle tabular-nums">
      <span>{children}</span>
      {actions === undefined ? null : <span className="flex items-center gap-2">{actions}</span>}
    </div>
  );
}

/**
 * A list's blocks that belong together, such as filter chips over the table they filter: 12px apart
 * instead of the page's 28px.
 */
export function TableGroup({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return <div className={cn("flex min-w-0 flex-col gap-3", className)}>{children}</div>;
}
