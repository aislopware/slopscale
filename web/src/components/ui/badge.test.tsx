import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { Badge } from "~/components/ui/badge.tsx";
import type { Tone } from "~/components/ui/status.tsx";

const tones: Tone[] = ["success", "warning", "danger", "info", "neutral"];

describe(Badge, () => {
  // The word and its tint carry the state; a dot or an icon in front of it is the tell of a
  // generated UI, and the badge's own corner radius is the controls', not a pill's.
  it.each(tones)("renders the %s state as a tinted word with nothing in front", async (tone) => {
    const view = await render(<Badge tone={tone}>State</Badge>);
    const badge = view.getByText("State").element();

    expect(view.container.textContent).toBe("State");
    expect(badge.querySelectorAll("svg, span[aria-hidden]")).toHaveLength(0);
    expect(getComputedStyle(badge).borderRadius).not.toBe("9999px");
  });

  it("tints each tone differently, and the tint is not the page", async () => {
    const view = await render(
      <>
        <Badge tone="success">Connected</Badge>
        <Badge tone="warning">Pending</Badge>
        <Badge tone="danger">Expired</Badge>
        <Badge tone="neutral">Disconnected</Badge>
      </>,
    );
    const background = (text: string): string =>
      getComputedStyle(view.getByText(text).element()).backgroundColor;
    const tints = ["Connected", "Pending", "Expired", "Disconnected"].map((text) =>
      background(text),
    );

    expect(new Set(tints).size).toBe(tints.length);
    for (const tint of tints) {
      expect(tint).not.toBe("rgba(0, 0, 0, 0)");
    }
  });
});
