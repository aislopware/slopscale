import { useState } from "react";

/** Where a cursor-paged list stands: the page shown out of what the server has sent so far. */
export interface CursorPaging {
  /** The page shown, from 1. */
  readonly page: number;
  readonly pageSize: number;
  /** How many rows the server has sent so far. */
  readonly loaded: number;
  /** The server has at least one more page beyond the rows loaded. */
  readonly hasMore: boolean;
  /** The next page is on its way. */
  readonly fetching: boolean;
  /** The last ask for the next page failed; asking again tries again. */
  readonly failed: boolean;
  readonly setPage: (page: number) => void;
}

export interface PageWindow<Row> extends CursorPaging {
  /** The rows on the page shown. */
  readonly rows: readonly Row[];
}

export interface PageWindowOptions<Row> {
  /** Every row the server has sent, in order, across the pages fetched so far. */
  readonly rows: readonly Row[];
  readonly pageSize: number;
  readonly hasMore: boolean;
  readonly fetching: boolean;
  /** The last fetch of the next page failed. */
  readonly failed: boolean;
  /** Asks the server for the page after the last one loaded. */
  readonly fetchMore: () => void;
  /** Goes back to the first page whenever this changes: the filters the rows are for. */
  readonly resetKey?: string;
}

/**
 * Pages a cursor-paged list the way a client-paged one pages: the rows already loaded are cut into
 * pages, and stepping past the last of them fetches the next from the server and shows it as it
 * lands. Previous pages stay loaded, so going back is instant. The fetch happens on the step, not
 * in an effect, so a page that fails to load is asked for again only when the reader steps again.
 */
export function usePageWindow<Row>({
  rows,
  pageSize,
  hasMore,
  fetching,
  failed,
  fetchMore,
  resetKey = "",
}: PageWindowOptions<Row>): PageWindow<Row> {
  // The page asked for, with the key it was asked for under: a page asked for under other filters
  // is not this list's page, so the list starts over at one without a reset of its own.
  const [asked, setAsked] = useState({ key: resetKey, page: 1 });
  const wanted = asked.key === resetKey ? asked.page : 1;
  const loadedPages = Math.max(1, Math.ceil(rows.length / pageSize));
  const page = Math.min(wanted, loadedPages);

  return {
    rows: rows.slice((page - 1) * pageSize, page * pageSize),
    page,
    pageSize,
    loaded: rows.length,
    hasMore,
    fetching: fetching && wanted > loadedPages,
    failed: failed && wanted > loadedPages,
    // The step reads this render's values: the caller renders again on every change, so the
    // handler it holds is never older than the list it pages.
    setPage: (next) => {
      setAsked({ key: resetKey, page: next });

      if (next > loadedPages && hasMore && !fetching) {
        fetchMore();
      }
    },
  };
}
