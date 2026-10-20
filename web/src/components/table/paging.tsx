import { Pagination } from "@cloudflare/kumo/components/pagination";
import type { ReactElement, ReactNode } from "react";

import type { CursorPaging } from "~/components/table/page-window.ts";

/** The smallest page on offer, the first of the band's "Per page" choices. */
export const smallestPageSize = 25;

const enDash = "–";

function rangeText(page: number, pageSize: number, count: number): string {
  const first = (page - 1) * pageSize + 1;
  const last = Math.min(page * pageSize, count);

  return `${first}${enDash}${last}`;
}

/**
 * The band at a table's foot, Kumo's Pagination as the dashboard lays it out: "Showing 1-10 of 120"
 * in 13px subtle figures, the rows per page, and the joined pager on the right.
 */
function Band({ children }: { readonly children: ReactNode }): ReactElement {
  return (
    <div className="text-sm text-kumo-subtle tabular-nums [&_[data-slot=pagination-info]]:whitespace-nowrap">
      {children}
    </div>
  );
}

export interface PagingBandProps {
  readonly page: number;
  readonly pageSize: number;
  readonly total: number;
  readonly setPage: (page: number) => void;
  readonly setPageSize: (size: number) => void;
  /** Leaves the band's "Per page" out, for a table whose toolbar sets it (a log's "Limit"). */
  readonly hidePageSize?: boolean;
}

/**
 * Paging over rows the browser already holds, so the total is known and every page is reachable. It
 * is the Zero Trust list footer of design spec §13.3: "Showing 1-5 of 5", a divider, "Per page:"
 * and its select, and the joined pager on the right, the same on a list of five rows as on one of
 * five hundred, so every list ends the same way. A phone keeps the range and the pager.
 */
export function PagingBand({
  page,
  pageSize,
  total,
  setPage,
  setPageSize,
  hidePageSize = false,
}: PagingBandProps): ReactElement {
  return (
    <Band>
      <Pagination page={page} perPage={pageSize} totalCount={total} setPage={setPage}>
        <Pagination.Info>
          {() => `Showing ${rangeText(page, pageSize, total)} of ${total}`}
        </Pagination.Info>
        {hidePageSize ? null : (
          <>
            <Pagination.Separator className="max-sm:hidden" />
            <Pagination.PageSize
              className="max-sm:hidden"
              value={pageSize}
              onChange={setPageSize}
            />
          </>
        )}
        <Pagination.Controls controls="full" pageSelector="input" className="ml-auto grow-0" />
      </Pagination>
    </Band>
  );
}

export interface CursorBandProps {
  readonly paging: CursorPaging;
  /**
   * Leaves the band's "Per page" out, for a list whose toolbar already sets the rows per page (the
   * audit log's "Limit").
   */
  readonly hidePageSize?: boolean;
}

/** What follows the range while more pages may exist: on their way, or not come. */
function moreText(fetching: boolean, failed: boolean): string {
  if (fetching) {
    return ", loading more";
  }

  return failed ? ". The next page did not load" : "";
}

/**
 * Paging over rows the server hands out a page at a time, in the same band as a table the browser
 * holds whole: "Showing 1-25 of 25", "Per page:" and the pager, a short list included. Until the
 * server says there are no more, the total is unknown, so the range names what has loaded and the
 * pager offers Previous and Next.
 */
export function CursorBand({ paging, hidePageSize = false }: CursorBandProps): ReactElement | null {
  const { page, pageSize, loaded, hasMore, fetching, failed, setPage, setPageSize } = paging;

  if (loaded === 0) {
    return null;
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
        {hidePageSize ? null : (
          <>
            <Pagination.Separator className="max-sm:hidden" />
            <Pagination.PageSize
              className="max-sm:hidden"
              value={pageSize}
              onChange={setPageSize}
            />
          </>
        )}
        <Pagination.Controls
          controls={hasMore ? "simple" : "full"}
          pageSelector="input"
          className="ml-auto grow-0"
        />
      </Pagination>
    </Band>
  );
}
