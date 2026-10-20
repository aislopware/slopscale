import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { MachinesEmpty, MachinesFirstRun } from "~/components/machines/empty.tsx";

describe(MachinesEmpty, () => {
  it("says where machines come from, and leaves the toolbar to offer one", async () => {
    const screen = await render(
      <MachinesEmpty
        total={0}
        status="all"
        narrowed={false}
        onClearFilters={() => {
          // Nothing is filtered.
        }}
      />,
    );

    await expect.element(screen.getByText("No machines yet")).toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "Add machine" }))
      .not.toBeInTheDocument();
  });

  it("says nothing is waiting on the approval tab", async () => {
    const clear = vi.fn<() => void>();
    const screen = await render(
      <MachinesEmpty total={3} status="pending" narrowed={false} onClearFilters={clear} />,
    );

    await expect.element(screen.getByText("No machines need approval")).toBeVisible();
    await screen.getByRole("button", { name: "View all machines" }).click();
    expect(clear).toHaveBeenCalledOnce();
  });

  it("clears the filters that hid every row", async () => {
    const clear = vi.fn<() => void>();
    const screen = await render(
      <MachinesEmpty total={3} status="pending" narrowed onClearFilters={clear} />,
    );

    await expect.element(screen.getByText("No machines match")).toBeVisible();
    await screen.getByRole("button", { name: "Clear filters" }).click();
    expect(clear).toHaveBeenCalledOnce();
  });
});

describe(MachinesFirstRun, () => {
  it("offers the first machine to a caller who may hand out keys", async () => {
    const add = vi.fn<() => void>();
    const screen = await render(<MachinesFirstRun onAddMachine={add} />);

    await expect.element(screen.getByRole("heading", { name: "No machines yet" })).toBeVisible();
    await screen.getByRole("button", { name: "Add your first machine" }).click();
    expect(add).toHaveBeenCalledOnce();
  });

  it("offers nothing it would refuse to a caller who may not", async () => {
    const screen = await render(<MachinesFirstRun />);

    await expect.element(screen.getByRole("heading", { name: "No machines yet" })).toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "Add your first machine" }))
      .not.toBeInTheDocument();
  });
});
