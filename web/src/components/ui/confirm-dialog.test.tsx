import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";

describe(ConfirmDialog, () => {
  it("confirms and cancels", async () => {
    const onConfirm = vi.fn<() => void>();
    const onOpenChange = vi.fn<(open: boolean) => void>();
    const screen = await render(
      <ConfirmDialog
        open
        onOpenChange={onOpenChange}
        title="Remove machine?"
        description="It can register again later."
        confirmLabel="Remove"
        onConfirm={onConfirm}
      />,
    );

    await expect
      .element(screen.getByRole("alertdialog", { name: "Remove machine?" }))
      .toBeVisible();
    await screen.getByRole("button", { name: "Remove" }).click();
    expect(onConfirm).toHaveBeenCalledOnce();

    await screen.getByRole("button", { name: "Cancel" }).click();
    expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything());
  });

  it("shows the error it is given", async () => {
    const screen = await render(
      <ConfirmDialog
        open
        onOpenChange={vi.fn<(open: boolean) => void>()}
        title="Expire key?"
        error="node is already expired"
        onConfirm={vi.fn<() => void>()}
      />,
    );

    await expect.element(screen.getByText("node is already expired")).toBeVisible();
  });
});
