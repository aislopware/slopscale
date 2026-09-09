import { useState } from "react";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";

interface Machine {
  readonly id: string;
  readonly name: string;
  readonly peers: number;
}

const helper = createAppColumnHelper<Machine>();

const columns = helper.columns([
  helper.accessor((machine) => machine.name, {
    id: "name",
    header: "Machine",
    enableSorting: true,
  }),
  helper.accessor((machine) => machine.peers, {
    id: "peers",
    header: "Peers",
    enableSorting: true,
    enableGlobalFilter: false,
    meta: { numeric: true },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row }) => (
      <button type="button" aria-label={`Actions for ${row.original.name}`}>
        …
      </button>
    ),
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

const total = 120;
const pad = 3;

/** Built once, so a re-render hands the table the same rows and not a new collection. */
const machines: readonly Machine[] = Array.from({ length: total }, (_, index) => ({
  id: String(index + 1),
  name: `machine-${String(index + 1).padStart(pad, "0")}`,
  peers: index,
}));

function Machines({ count = total }: { readonly count?: number }): ReactElement {
  const [query, setQuery] = useState("");
  const rows = machines.slice(0, count);
  const table = useAppTable({
    data: rows,
    columns,
    getRowId: (machine) => machine.id,
    state: { globalFilter: query },
  });

  return (
    <>
      <button
        type="button"
        onClick={() => {
          setQuery("machine-11");
        }}
      >
        Narrow
      </button>
      <table.AppTable>
        <DataTable
          empty={<p>No machines</p>}
          footer={<TableFooter>{`Showing ${table.getRowModel().rows.length}`}</TableFooter>}
        />
      </table.AppTable>
    </>
  );
}

describe("a paged table", () => {
  it("names the range it shows and walks to the next page", async () => {
    const screen = await render(<Machines />);

    await expect.element(screen.getByText("Showing 1–50 of 120")).toBeVisible();
    await expect.element(screen.getByText("machine-001")).toBeVisible();
    await expect.element(screen.getByText("machine-051")).not.toBeInTheDocument();

    await screen.getByRole("button", { name: "Next page" }).click();

    await expect.element(screen.getByText("Showing 51–100 of 120")).toBeVisible();
    await expect.element(screen.getByText("machine-051")).toBeVisible();
  });

  it("keeps the sort while paging", async () => {
    const screen = await render(<Machines />);
    // Sorting names the direction inside the header button, so the button's name grows with it.
    const header = screen.getByRole("button", { name: /^Machine/u });

    await header.click();
    await header.click();

    await expect.element(screen.getByText("machine-120")).toBeVisible();

    await screen.getByRole("button", { name: "Next page" }).click();

    await expect.element(screen.getByLabelText("Sorted descending")).toBeVisible();
    await expect.element(screen.getByText("machine-070")).toBeVisible();
  });

  it("goes back to the first page when the filter narrows the rows", async () => {
    const screen = await render(<Machines />);

    await screen.getByRole("button", { name: "Next page" }).click();

    await expect.element(screen.getByText("Showing 51–100 of 120")).toBeVisible();

    await screen.getByRole("button", { name: "Narrow" }).click();

    await expect.element(screen.getByText("machine-110")).toBeVisible();
    await expect.element(screen.getByText("Showing 10")).toBeVisible();
  });

  it("leaves the page's own count on the band while everything fits", async () => {
    const oneScreenful = 20;
    const screen = await render(<Machines count={oneScreenful} />);

    await expect.element(screen.getByText("Showing 20")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Next page" })).not.toBeInTheDocument();
  });
});

/** The nearest element of that kind, as the assertions below need it to exist. */
function enclosing(element: Element, selector: string): Element {
  const found = element.closest(selector);

  if (found === null) {
    throw new Error(`no ${selector} around ${element.tagName}`);
  }

  return found;
}

describe("a table's columns", () => {
  it("right aligns a numeric column with tabular figures", async () => {
    const screen = await render(<Machines count={2} />);
    const cell = screen.getByRole("cell", { name: "0" }).element();

    expect(getComputedStyle(cell).textAlign).toBe("right");
    expect(getComputedStyle(cell).fontVariantNumeric).toContain("tabular-nums");
  });

  it("pins the row action column to the right edge", async () => {
    const screen = await render(<Machines count={2} />);
    const menu = screen.getByRole("button", { name: "Actions for machine-001" }).element();
    const cell = enclosing(menu, "td");

    expect(getComputedStyle(cell).position).toBe("sticky");
    expect(getComputedStyle(cell).right).toBe("0px");
  });

  it("steps the outer cells in to where the band's text starts", async () => {
    const screen = await render(<Machines count={2} />);
    const first = screen.getByRole("cell", { name: "machine-001" }).element();

    expect(getComputedStyle(first).paddingLeft).toBe("20px");
  });

  it("draws no stripe on the second row", async () => {
    const screen = await render(
      <>
        <div data-testid="elevated" className="bg-kumo-elevated" />
        <Machines count={4} />
      </>,
    );
    const elevated = screen.getByTestId("elevated").element();
    const row = enclosing(screen.getByText("machine-002").element(), "tr");

    expect(getComputedStyle(row).backgroundColor).not.toBe(
      getComputedStyle(elevated).backgroundColor,
    );
  });
});
