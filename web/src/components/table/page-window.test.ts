import { describe, expect, it, vi } from "vitest";
import { renderHook } from "vitest-browser-react";

import { usePageWindow } from "~/components/table/page-window.ts";
import type { PageWindow, PageWindowOptions } from "~/components/table/page-window.ts";

const pageSize = 2;

function noFetch(): void {
  // The default asks nothing; tests that care pass a mock.
}

function rowsUpTo(count: number): readonly number[] {
  return Array.from({ length: count }, (_, index) => index + 1);
}

function options(overrides: Partial<PageWindowOptions<number>>): PageWindowOptions<number> {
  return {
    rows: rowsUpTo(pageSize),
    pageSize,
    hasMore: true,
    fetching: false,
    failed: false,
    fetchMore: noFetch,
    ...overrides,
  };
}

/** RenderHook may call the hook without props on a bare rerender; the tests always pass them. */
function useWindow(props = options({})): PageWindow<number> {
  return usePageWindow(props);
}

describe(usePageWindow, () => {
  it("fetches on the step past the rows loaded and shows the page as it lands", async () => {
    const fetchMore = vi.fn<() => void>();
    const { result, rerender, act } = await renderHook(useWindow, {
      initialProps: options({ fetchMore }),
    });

    await act(() => {
      result.current.setPage(2);
    });
    expect(fetchMore).toHaveBeenCalledOnce();
    expect(result.current.page).toBe(1);

    await rerender(options({ fetchMore, fetching: true }));
    expect(result.current.fetching).toBe(true);

    await rerender(options({ fetchMore, rows: rowsUpTo(pageSize * 2) }));
    expect(result.current).toMatchObject({ page: 2, rows: [3, 4], fetching: false });
  });

  it("does not ask again on its own after a failed fetch, only on the next step", async () => {
    const fetchMore = vi.fn<() => void>();
    const { result, rerender, act } = await renderHook(useWindow, {
      initialProps: options({ fetchMore }),
    });

    await act(() => {
      result.current.setPage(2);
    });
    await rerender(options({ fetchMore, failed: true }));
    expect(fetchMore).toHaveBeenCalledOnce();
    expect(result.current.failed).toBe(true);

    await act(() => {
      result.current.setPage(2);
    });
    expect(fetchMore).toHaveBeenCalledTimes(2);
  });

  it("cuts the rows loaded into pages of the size picked, from the first", async () => {
    const { result, act } = await renderHook(useWindow, {
      initialProps: options({ rows: rowsUpTo(pageSize * 3), hasMore: false }),
    });

    await act(() => {
      result.current.setPage(3);
    });
    await act(() => {
      result.current.setPageSize(pageSize * 3);
    });
    expect(result.current).toMatchObject({ page: 1, pageSize: pageSize * 3, rows: rowsUpTo(6) });
  });

  it("starts over at the first page when the key changes", async () => {
    const { result, rerender, act } = await renderHook(useWindow, {
      initialProps: options({ rows: rowsUpTo(pageSize * 2), resetKey: "a" }),
    });

    await act(() => {
      result.current.setPage(2);
    });
    expect(result.current.page).toBe(2);

    await rerender(options({ rows: rowsUpTo(pageSize * 2), resetKey: "b" }));
    expect(result.current.page).toBe(1);
  });
});
