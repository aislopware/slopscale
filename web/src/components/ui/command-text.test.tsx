import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { CommandBox, ShellText } from "~/components/ui/command-text.tsx";

const docker = "docker run -d \\\n  -e TS_AUTHKEY=<key> \\\n  tailscale/tailscale";

describe(ShellText, () => {
  it("weights the program, its flags and a placeholder differently", async () => {
    const screen = await render(<ShellText command="tailscale up --authkey=<key>" />);

    await expect.element(screen.getByText("tailscale")).toHaveClass("font-semibold");
    await expect.element(screen.getByText("--authkey")).toHaveClass("text-kumo-link");
    await expect.element(screen.getByText("<key>")).toHaveClass("text-kumo-warning");
  });
});

describe(CommandBox, () => {
  it("shows a wrapped command with the line breaks it was written with", async () => {
    const screen = await render(<CommandBox command={docker} wrap />);
    const box = screen.getByRole("button", { name: "Copy command" });

    await expect.element(box).toBeVisible();
    expect(box.element().textContent).toBe(docker);
    // A word inherits the code's white space, and the last one may break inside.
    await expect
      .element(screen.getByText("tailscale/tailscale"))
      .toHaveStyle({ whiteSpace: "pre-wrap", overflowWrap: "anywhere" });
  });

  it("stays on one line, cut short, when not wrapping", async () => {
    const screen = await render(
      <div style={{ width: 120 }}>
        <CommandBox command="tailscale up --login-server=http://hs.example --authkey=<key>" />
      </div>,
    );
    const code = screen.getByRole("button", { name: "Copy command" }).element();

    expect(code.getBoundingClientRect().width).toBeLessThanOrEqual(120);
    expect(code.scrollWidth).toBeLessThanOrEqual(code.clientWidth);
  });
});
