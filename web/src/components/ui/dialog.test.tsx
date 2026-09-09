import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import { DialogContent, DialogFooter, DialogRoot } from "~/components/ui/dialog.tsx";

/** More paragraphs than any screen shows at once. */
const paragraphs = 40;

function TallDialog(): React.ReactElement {
  return (
    <DialogRoot open onOpenChange={vi.fn<(open: boolean) => void>()}>
      <DialogContent title="A long form" description="Scroll for the rest.">
        {Array.from({ length: paragraphs }, (_, index) => (
          // Filler lines have no identity beyond their position.
          // eslint-disable-next-line react/no-array-index-key
          <p key={index}>Line {index + 1}</p>
        ))}
        <DialogFooter>
          <Button variant="primary">Save</Button>
        </DialogFooter>
      </DialogContent>
    </DialogRoot>
  );
}

function FormDialog({
  onSubmit,
  disabled = false,
}: {
  /** Called with the text of the button that submitted, or "" for an implicit submission. */
  readonly onSubmit: (submitter: string) => void;
  readonly disabled?: boolean;
}): React.ReactElement {
  return (
    <DialogRoot open onOpenChange={vi.fn<(open: boolean) => void>()}>
      <DialogContent title="Rename">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit(event.nativeEvent.submitter?.textContent ?? "");
          }}
        >
          <Input label="Name" defaultValue="laptop" />
          <DialogFooter submitDisabled={disabled}>
            <Button variant="secondary">Cancel</Button>
            <Button type="submit" variant="primary" disabled={disabled}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </DialogRoot>
  );
}

describe(DialogContent, () => {
  it("keeps the footer in reach when the body is taller than the screen", async () => {
    const screen = await render(<TallDialog />);
    const dialog = screen.getByRole("dialog", { name: "A long form" });
    const save = screen.getByRole("button", { name: "Save" });

    await expect.element(dialog).toBeVisible();
    await expect.element(save).toBeVisible();

    const box = dialog.element().getBoundingClientRect();
    const button = save.element().getBoundingClientRect();

    // The dialog stops short of the bottom of the screen, and the button is inside it.
    expect(box.bottom).toBeLessThan(window.innerHeight);
    expect(button.bottom).toBeLessThanOrEqual(box.bottom);
    await expect.element(screen.getByText("Line 1")).toBeInViewport();
    await expect.element(screen.getByText(`Line ${paragraphs}`)).not.toBeInViewport();
  });

  it("submits the form from the band, by button and by Enter", async () => {
    const onSubmit = vi.fn<(submitter: string) => void>();
    const screen = await render(<FormDialog onSubmit={onSubmit} />);

    // The band button is the form's own submitter, so a handler can tell which button it was.
    await screen.getByRole("button", { name: "Save" }).click();
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith("Save");

    await screen.getByRole("button", { name: "Cancel" }).click();
    expect(onSubmit).toHaveBeenCalledOnce();

    await screen.getByRole("textbox", { name: "Name" }).fill("desk");
    await userEvent.keyboard("{Enter}");
    expect(onSubmit).toHaveBeenCalledTimes(2);
  });

  it("does not submit by Enter while the button is disabled", async () => {
    const onSubmit = vi.fn<(submitter: string) => void>();
    const screen = await render(<FormDialog disabled onSubmit={onSubmit} />);

    await expect.element(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await screen.getByRole("textbox", { name: "Name" }).fill("desk");
    await userEvent.keyboard("{Enter}");

    expect(onSubmit).not.toHaveBeenCalled();
  });
});
