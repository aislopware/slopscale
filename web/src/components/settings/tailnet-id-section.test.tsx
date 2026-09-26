import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { TailnetIDSection } from "~/components/settings/tailnet-id-section.tsx";

describe(TailnetIDSection, () => {
  it("shows the tailnet ID as a value to copy", async () => {
    const screen = await render(<TailnetIDSection tailnetId="T4bXk29QaZmCNTRL" />);

    await expect.element(screen.getByText("Tailnet ID")).toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "Copy T4bXk29QaZmCNTRL" }))
      .toBeVisible();
    await expect.element(screen.getByText("CurrentTailnet.StableID")).toBeVisible();
  });
});
