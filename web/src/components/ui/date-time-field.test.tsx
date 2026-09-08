import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { DateTimeField } from "~/components/ui/date-time-field.tsx";

describe(DateTimeField, () => {
  it("shows the instant it is given and clears it", async () => {
    const onChange = vi.fn<(value: string) => void>();
    const screen = await render(
      <DateTimeField
        label="Expires"
        emptyLabel="Never"
        value="2026-09-15T21:05"
        onChange={onChange}
      />,
    );

    await expect.element(screen.getByRole("button", { name: "Sep 15, 2026" })).toBeVisible();
    await screen.getByRole("button", { name: "Clear" }).click();

    expect(onChange).toHaveBeenCalledWith("");
  });

  // The three dialogs still send what a datetime-local input held, so the shape may not drift.
  it("keeps the date and time in one datetime-local value", async () => {
    const onChange = vi.fn<(value: string) => void>();
    const screen = await render(
      <DateTimeField label="Expires" value="2026-09-15T21:05" onChange={onChange} />,
    );

    await screen.getByRole("combobox", { name: "Expires hour" }).click();
    await screen.getByRole("option", { name: "09" }).click();

    expect(onChange).toHaveBeenCalledWith("2026-09-15T09:05");
  });

  it("has no time to pick until a date is chosen", async () => {
    const screen = await render(
      <DateTimeField
        label="Until"
        emptyLabel="No end"
        value=""
        onChange={vi.fn<(value: string) => void>()}
      />,
    );

    await expect.element(screen.getByRole("button", { name: "No end" })).toBeVisible();
    await expect.element(screen.getByRole("combobox", { name: "Until hour" })).toBeDisabled();
  });
});
