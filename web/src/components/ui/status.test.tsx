import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Tone } from "~/components/ui/status.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";

const tones: Tone[] = ["success", "warning", "danger", "info", "neutral"];

describe(Status, () => {
  // A tone whose class names no Kumo token would render text with no dot, which is how
  // "Disconnected" lost its dot once; so every tone is checked for a painted dot.
  it.each(tones)("paints the %s dot", async (tone) => {
    const screen = await render(<Status tone={tone}>State</Status>);
    const dots = screen.container.querySelectorAll("span[aria-hidden]");

    expect(dots).toHaveLength(1);
    expect(Array.from(dots, (dot) => getComputedStyle(dot).backgroundColor)).not.toContain(
      "rgba(0, 0, 0, 0)",
    );
  });
});

describe(StatusDetail, () => {
  // The reason is why the state is worth reading; a keyboard has to reach it, not only a pointer.
  it("keeps the reason a button away instead of on the line", async () => {
    const screen = await render(
      <StatusDetail
        tone="danger"
        label="Failed"
        title="The machine did not answer"
        detail="the client timed out"
      />,
    );

    await expect.element(screen.getByText("Failed")).toBeVisible();
    expect(screen.container.textContent).not.toContain("the client timed out");

    await screen.getByRole("button", { name: "Failed" }).click();

    await expect.element(screen.getByText("The machine did not answer")).toBeVisible();
    await expect.element(screen.getByText("the client timed out")).toBeVisible();
  });
});
