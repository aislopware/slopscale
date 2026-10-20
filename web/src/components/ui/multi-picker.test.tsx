import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import type { PickerItem } from "~/components/ui/multi-picker.tsx";

const items: PickerItem[] = [
  { value: "1", label: "admins" },
  { value: "2", label: "engineers" },
];

describe(MultiPicker, () => {
  it("adds the highlighted match on Enter without submitting the form", async () => {
    const onValueChange = vi.fn<(value: string[]) => void>();
    const onSubmit = vi.fn<() => void>();
    const screen = await render(
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <MultiPicker
          label="Groups"
          placeholder="Add groups"
          items={items}
          value={[]}
          onValueChange={onValueChange}
        />
      </form>,
    );

    await screen.getByRole("combobox").click();
    await userEvent.keyboard("eng");
    await userEvent.keyboard("{Enter}");

    expect(onValueChange).toHaveBeenLastCalledWith(["2"]);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps the form from submitting on Enter when nothing matches", async () => {
    const onValueChange = vi.fn<(value: string[]) => void>();
    const onSubmit = vi.fn<() => void>();
    const screen = await render(
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <MultiPicker
          label="Groups"
          placeholder="Add groups"
          items={items}
          value={["1"]}
          onValueChange={onValueChange}
        />
      </form>,
    );

    await screen.getByRole("combobox").click();
    await userEvent.keyboard("nobody");
    await userEvent.keyboard("{Enter}");

    expect(onSubmit).not.toHaveBeenCalled();
    expect(onValueChange).not.toHaveBeenCalled();
  });

  it("keeps every chip when Escape is pressed on a closed list", async () => {
    const onValueChange = vi.fn<(value: string[]) => void>();
    const screen = await render(
      <MultiPicker
        label="Groups"
        placeholder="Add groups"
        items={items}
        value={["1", "2"]}
        onValueChange={onValueChange}
      />,
    );

    await screen.getByRole("combobox").click();
    await userEvent.keyboard("{Escape}");
    await userEvent.keyboard("{Escape}");

    expect(onValueChange).not.toHaveBeenCalled();
    await expect.element(screen.getByText("admins")).toBeVisible();
    await expect.element(screen.getByText("engineers")).toBeVisible();
  });

  it("marks the input invalid while it has an error and clears the mark with it", async () => {
    const screen = await render(
      <MultiPicker
        label="Groups"
        placeholder="Add groups"
        items={items}
        value={[]}
        onValueChange={vi.fn<(value: string[]) => void>()}
        error="Pick at least one group"
      />,
    );

    await expect.element(screen.getByRole("combobox")).toHaveAttribute("aria-invalid", "true");
    await expect.element(screen.getByText("Pick at least one group")).toBeVisible();

    await screen.rerender(
      <MultiPicker
        label="Groups"
        placeholder="Add groups"
        items={items}
        value={[]}
        onValueChange={vi.fn<(value: string[]) => void>()}
      />,
    );

    await expect.element(screen.getByRole("combobox")).not.toHaveAttribute("aria-invalid");
  });
});
