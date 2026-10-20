import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { ConfirmDialog, DeleteDialog } from "~/components/ui/confirm-dialog.tsx";

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

describe(DeleteDialog, () => {
  // The dashboard asks for the name before it deletes a named resource; anything but the exact
  // name, a different case included, keeps the action locked.
  it("holds the delete back until the name is typed exactly", async () => {
    const onDelete = vi.fn<() => void>();
    const screen = await render(
      <DeleteDialog
        open
        onOpenChange={vi.fn<(open: boolean) => void>()}
        resourceType="machine"
        resourceName="backup-nas"
        deleteButtonText="Remove machine"
        onDelete={onDelete}
      />,
    );
    const action = screen.getByRole("button", { name: "Remove machine" });
    const input = screen.getByRole("textbox", { name: "Type backup-nas to confirm" });

    await expect
      .element(screen.getByRole("alertdialog", { name: "Delete backup-nas?" }))
      .toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Close" })).not.toBeInTheDocument();
    await expect.element(action).toBeDisabled();

    await input.fill("Backup-NAS");
    await expect.element(action).toBeDisabled();

    await input.fill("backup-nas");
    await expect.element(action).toBeEnabled();
    await action.click();
    expect(onDelete).toHaveBeenCalledOnce();
  });

  it("shows why the delete failed and keeps the dialog open", async () => {
    const screen = await render(
      <DeleteDialog
        open
        onOpenChange={vi.fn<(open: boolean) => void>()}
        resourceType="user"
        resourceName="alice"
        errorMessage="user owns machines"
        onDelete={vi.fn<() => void>()}
      />,
    );

    await expect.element(screen.getByText("user owns machines")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Delete user" })).toBeDisabled();
  });
});
