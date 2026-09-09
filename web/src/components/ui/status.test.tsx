import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Tone } from "~/components/ui/status.tsx";
import { Status } from "~/components/ui/status.tsx";

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
