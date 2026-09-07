import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { MachinesEmpty } from "~/components/machines/empty.tsx";

describe(MachinesEmpty, () => {
  it("offers the first machine when the tailnet is empty", async () => {
    const add = vi.fn<() => void>();
    const screen = await render(
      <MachinesEmpty
        total={0}
        status="all"
        narrowed={false}
        canCreateKeys
        onAddMachine={add}
        onClearFilters={() => {
          // Nothing is filtered.
        }}
      />,
    );

    await screen.getByRole("button", { name: "Add machine" }).click();
    expect(add).toHaveBeenCalledOnce();
  });

  it("leaves out the action a caller may not take", async () => {
    const screen = await render(
      <MachinesEmpty
        total={0}
        status="all"
        narrowed={false}
        canCreateKeys={false}
        onAddMachine={() => {
          // Not offered.
        }}
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
      <MachinesEmpty
        total={3}
        status="pending"
        narrowed={false}
        canCreateKeys
        onAddMachine={() => {
          // Machines exist.
        }}
        onClearFilters={clear}
      />,
    );

    await expect.element(screen.getByText("No machines need approval")).toBeVisible();
    await screen.getByRole("button", { name: "View all machines" }).click();
    expect(clear).toHaveBeenCalledOnce();
  });

  it("clears the filters that hid every row", async () => {
    const clear = vi.fn<() => void>();
    const screen = await render(
      <MachinesEmpty
        total={3}
        status="pending"
        narrowed
        canCreateKeys
        onAddMachine={() => {
          // Machines exist.
        }}
        onClearFilters={clear}
      />,
    );

    await expect.element(screen.getByText("No machines match")).toBeVisible();
    await screen.getByRole("button", { name: "Clear filters" }).click();
    expect(clear).toHaveBeenCalledOnce();
  });
});
