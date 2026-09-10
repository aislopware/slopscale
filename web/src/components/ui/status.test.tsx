import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Tone } from "~/components/ui/status.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";

const tones: Tone[] = ["success", "warning", "danger", "info", "neutral"];

describe(Status, () => {
  // A state is words alone: a dot or a pill in front of it is the tell of a generated UI, and the
  // colour of the word already says which rows to look at.
  it.each(tones)("renders the %s state as words with nothing in front", async (tone) => {
    const view = await render(<Status tone={tone}>State</Status>);

    expect(view.container.querySelectorAll("span[aria-hidden]")).toHaveLength(0);
    expect(view.container.textContent).toBe("State");
  });

  it("colours only the states that need attention", async () => {
    const view = await render(
      <>
        <Status tone="danger">Failed</Status>
        <Status tone="warning">Expiring</Status>
        <Status tone="success">Connected</Status>
        <Status tone="neutral">Disconnected</Status>
      </>,
    );
    const colour = (text: string): string => getComputedStyle(view.getByText(text).element()).color;

    expect(colour("Failed")).not.toBe(colour("Connected"));
    expect(colour("Expiring")).not.toBe(colour("Connected"));
    expect(colour("Disconnected")).not.toBe(colour("Connected"));
    expect(colour("Failed")).not.toBe(colour("Expiring"));
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
