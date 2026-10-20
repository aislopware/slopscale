import {
  TableOfContents,
  useTableOfContentsActiveId,
} from "@cloudflare/kumo/components/table-of-contents";
import { cn } from "@cloudflare/kumo/utils";
import { useId } from "react";
import type { ReactElement, ReactNode } from "react";

import { DocsBadge } from "~/components/ui/docs-badge.tsx";

/** A part of a settings page, as its table of contents lists it. */
export interface SettingsPart {
  /** The id of the part's `SettingsGroup`, which the contents link to. */
  readonly id: string;
  readonly label: string;
  /** The danger zone: listed last, in the danger colour. */
  readonly danger?: boolean;
}

/** How far below the top bar a part comes to rest when the contents jump to it. */
const restOffset = 72;

/**
 * A settings page as the dashboard lays one out (Worker settings, Zero Trust settings): the parts
 * down the left, each a heading over a settings card, and Kumo's table of contents on the right,
 * sticky under the top bar, its blue rail on the part being read. The contents step aside below
 * 1024px of content, where the page is one column anyway.
 */
export function SettingsLayout({
  parts,
  children,
}: {
  readonly parts: readonly SettingsPart[];
  readonly children: ReactNode;
}): ReactElement {
  const { activeId, selectSection } = useTableOfContentsActiveId({
    ids: parts.map((part) => part.id),
    offset: restOffset,
  });
  const current = activeId ?? parts[0]?.id;

  return (
    <div className="grid gap-x-8 gap-y-10 @5xl:grid-cols-[minmax(0,1fr)_11rem]">
      <div className="flex min-w-0 flex-col gap-10">{children}</div>
      <TableOfContents
        aria-label="On this page"
        className="hidden self-start @5xl:sticky @5xl:top-[calc(var(--app-header-height,0px)+2rem)] @5xl:block"
      >
        {/* Kumo sets the label in small capitals; the console writes every heading in sentence
            case. */}
        <TableOfContents.Title className="tracking-normal normal-case">
          On this page
        </TableOfContents.Title>
        <TableOfContents.List>
          {parts.map((part) => (
            <TableOfContents.Item
              key={part.id}
              href={`#${part.id}`}
              active={current === part.id}
              className={cn(part.danger === true && "text-kumo-danger hover:text-kumo-danger")}
              onClick={(event) => {
                // The router owns the address; the jump is the page's own, so it scrolls and
                // leaves the address alone.
                event.preventDefault();
                document
                  .querySelector(`#${CSS.escape(part.id)}`)
                  ?.scrollIntoView({ behavior: "smooth" });
                selectSection(part.id);
              }}
            >
              {part.label}
            </TableOfContents.Item>
          ))}
        </TableOfContents.List>
      </TableOfContents>
    </div>
  );
}

/**
 * A titled block on a page, the way Zero Trust heads the parts of a page that are not cards of
 * their own: a 16px semibold heading with an optional "View docs" pill and a subtle sentence under
 * it, small controls on the right, then the block itself (a settings card, a table in its own
 * frame). The heading sits on the page, not on a band, so nothing wraps the block in a second
 * frame. With an id it is a place a link or a table of contents can jump to.
 */
export function PageSection({
  id,
  title,
  docs,
  description,
  actions,
  children,
}: {
  readonly id?: string;
  readonly title: ReactNode;
  /** The documentation page that explains the part, under `docs/content`. */
  readonly docs?: string;
  readonly description?: ReactNode;
  /** Small controls on the heading's right, such as an "Add" button. */
  readonly actions?: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  const generated = useId();
  const headingId = `${id ?? generated}-heading`;

  return (
    <section
      id={id}
      aria-labelledby={headingId}
      className="flex min-w-0 scroll-mt-[calc(var(--app-header-height,0px)+1rem)] flex-col gap-3"
    >
      <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 id={headingId} className="text-lg font-semibold text-kumo-default">
              {title}
            </h2>
            {docs === undefined ? null : <DocsBadge path={docs} />}
          </div>
          {description === undefined ? null : (
            <p className="max-w-[750px] text-pretty text-kumo-subtle">{description}</p>
          )}
        </div>
        {actions === undefined ? null : (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
      {children}
    </section>
  );
}

/**
 * One part of a settings page: a `PageSection` over its settings card, with the id the table of
 * contents links to.
 */
export function SettingsGroup(
  props: Parameters<typeof PageSection>[0] & { readonly id: string },
): ReactElement {
  return <PageSection {...props} />;
}
