import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { QrCode, qrDataUrl } from "~/components/ui/qr-code.tsx";

describe(qrDataUrl, () => {
  it("grows the symbol with the text", () => {
    const short = decodeURIComponent(qrDataUrl("hi"));
    const long = decodeURIComponent(qrDataUrl("x".repeat(200)));

    expect(short).toContain('viewBox="0 0 29 29"');
    expect(long).not.toContain('viewBox="0 0 29 29"');
  });
});

describe(QrCode, () => {
  it("is an image named by its label", async () => {
    const screen = await render(<QrCode text="hi" label="short" />);

    await expect.element(screen.getByRole("img", { name: "short" })).toBeVisible();
  });
});
