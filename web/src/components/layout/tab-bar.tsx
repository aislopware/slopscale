import { LinkButton } from "@cloudflare/kumo/components/button";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { cn } from "@cloudflare/kumo/utils";
import { BookOpenTextIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { docsUrl } from "~/lib/docs.ts";
import { AppLink } from "~/lib/link.tsx";

/** One tab of the bar: a page of its own at `href`. */
export interface TabBarTab {
  readonly value: string;
  readonly label: string;
  readonly href: string;
}

/**
 * The Zero Trust bar under the top bar (design spec §13.2, §13.15.2), for a sidebar branch's pages
 * and for the tabs of a detail page alike: a 36px segmented Kumo `Tabs` track on the left, each tab
 * a link to its own address, and on the right what can be done there and the documentation. It
 * spans the page edge to edge with a rule under it, lines its tabs up with the page's column and
 * sticks under the top bar; while it is on the page, the page grid raises `--app-header-height` by
 * its 53px (see `pageGridClass`), so sticky toolbars and rails inside the page rest under it.
 *
 * On a phone the actions wrap under the tabs and the bar, then taller than its 53px, scrolls away
 * with the page; a bar of tabs alone stays put.
 */
export function TabBar({
  label,
  tabs,
  value,
  actions,
  docs,
}: {
  /** What the tabs are, for the navigation landmark: "DNS", "Machine". */
  readonly label: string;
  readonly tabs: readonly TabBarTab[];
  /** The tab shown. */
  readonly value: string;
  /** Buttons at the right end, before the documentation. */
  readonly actions?: ReactNode;
  /** The documentation page under `docs/content`, and the button's words. */
  readonly docs?: { readonly path: string; readonly label: string };
}): ReactElement {
  return (
    <nav
      data-full-bleed
      data-sub-nav
      aria-label={label}
      className={cn(
        "top-(--app-bar-height,0px) z-10 -mt-8 border-b border-kumo-line bg-kumo-canvas",
        actions === undefined ? "sticky" : "md:sticky",
      )}
    >
      <div className="grid grid-cols-[minmax(var(--page-padding-x),1fr)_minmax(0,var(--page-max-width))_minmax(var(--page-padding-x),1fr)]">
        <div className="col-start-2 flex min-h-[52px] min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 py-2">
          <Tabs
            variant="segmented"
            className="min-w-0"
            value={value}
            tabs={tabs.map((tab) => ({
              value: tab.value,
              label: tab.label,
              render: <AppLink href={tab.href} />,
              nativeButton: false,
            }))}
          />
          {actions === undefined && docs === undefined ? null : (
            // The documentation button leaves a phone's bar, so a bar with nothing else keeps one line.
            <div
              className={cn(
                "flex max-w-full flex-wrap items-center gap-2",
                actions === undefined && "max-md:hidden",
              )}
            >
              {actions}
              {docs === undefined ? null : (
                <LinkButton
                  variant="secondary"
                  href={docsUrl(docs.path)}
                  external
                  icon={<BookOpenTextIcon aria-hidden />}
                  className="shrink-0 max-md:hidden"
                >
                  {docs.label}
                </LinkButton>
              )}
            </div>
          )}
        </div>
      </div>
    </nav>
  );
}
