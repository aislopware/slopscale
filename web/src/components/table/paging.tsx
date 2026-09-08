import { Pagination } from "@cloudflare/kumo/components/pagination";
import type { ReactElement, ReactNode } from "react";

import { plural } from "~/components/overview/plural.ts";
import type { CursorPaging } from "~/components/table/page-window.ts";
import { FrameBand } from "~/components/ui/frame.tsx";

/** The smallest page on offer. A table with fewer rows than this has nothing to page. */
export const smallestPageSize = 25;

const enDash = "–";

function rangeText(page: number, pageSize: number, count: number): string {
  const first = (page - 1) * pageSize + 1;
  const last = Math.min(page * pageSize, count);

  return `${first}${enDash}${last}`;
}

/**
 * The band under a table that pages: the range shown, how many rows a page holds and the way to the
 * other pages. The band and the frame's padding put the text where the rows' text starts.
 */
function Band({ children }: { readonly children: ReactNode }): ReactElement {
  return (
    <FrameBand className="px-5 [&_[data-slot=pagination-info]]:whitespace-nowrap">
      {children}
    </FrameBand>
  );
}

export interface PagingBandProps {
  readonly page: number;
  readonly pageSize: number;
  readonly total: number;
  readonly setPage: (page: number) => void;
  readonly setPageSize: (size: number) => void;
}

/** Paging over rows the browser already holds, so the total is known and every page is reachable. */
export function PagingBand({
  page,
  pageSize,
  total,
  setPage,
  setPageSize,
}: PagingBandProps): ReactElement {
  const pages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <Band>
      <Pagination page={page} perPage={pageSize} totalCount={total} setPage={setPage}>
        <Pagination.Info>
          {() => `Showing ${rangeText(page, pageSize, total)} of ${total}`}
        </Pagination.Info>
        <Pagination.PageSize
          className="ml-auto"
          label="Rows per page"
          value={pageSize}
          onChange={setPageSize}
        />
        {pages > 1 ? (
          <Pagination.Controls controls="full" pageSelector="input" className="grow-0" />
        ) : null}
      </Pagination>
    </Band>
  );
}

export interface CursorBandProps {
  readonly paging: CursorPaging;
  /** What one row is, for the count: "event", "recording". */
  readonly noun: string;
}

/** What follows the range while more pages may exist: on their way, or not come. */
function moreText(fetching: boolean, failed: boolean): string {
  if (fetching) {
    return ", loading more";
  }

  return failed ? ". The next page did not load; try Next again" : "";
}

/**
 * Paging over rows the server hands out a page at a time. Until the server says there are no more,
 * the total is unknown, so the band offers Previous and Next and names the range so far.
 */
export function CursorBand({ paging, noun }: CursorBandProps): ReactElement | null {
  const { page, pageSize, loaded, hasMore, fetching, failed, setPage } = paging;

  if (loaded === 0) {
    return null;
  }

  if (!hasMore && loaded <= pageSize) {
    return (
      <Band>
        <Pagination page={1} perPage={pageSize} totalCount={loaded} setPage={setPage}>
          <Pagination.Info>{() => `Showing all ${plural(loaded, noun)}`}</Pagination.Info>
        </Pagination>
      </Band>
    );
  }

  const info = hasMore
    ? `Showing ${rangeText(page, pageSize, loaded)}${moreText(fetching, failed)}`
    : `Showing ${rangeText(page, pageSize, loaded)} of ${loaded}`;

  return (
    <Band>
      <Pagination
        page={page}
        perPage={pageSize}
        {...(hasMore ? { hasNextPage: true } : { totalCount: loaded })}
        setPage={setPage}
      >
        <Pagination.Info>{() => info}</Pagination.Info>
        <Pagination.Controls
          controls={hasMore ? "simple" : "full"}
          pageSelector="input"
          className="grow"
        />
      </Pagination>
    </Band>
  );
}
