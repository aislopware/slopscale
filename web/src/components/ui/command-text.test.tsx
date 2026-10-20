import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { CommandBox, ShellText, maskSecret } from "~/components/ui/command-text.tsx";

const docker = "docker run -d \\\n  -e TS_AUTHKEY=<key> \\\n  tailscale/tailscale";

describe(ShellText, () => {
  it("weights the program, its flags and a placeholder differently", async () => {
    const screen = await render(<ShellText command="tailscale up --authkey=<key>" />);

    await expect.element(screen.getByText("tailscale")).toHaveClass("font-semibold");
    await expect.element(screen.getByText("--authkey")).toHaveClass("text-kumo-link");
    await expect.element(screen.getByText("<key>")).toHaveClass("text-kumo-warning");
  });
});

describe(maskSecret, () => {
  it("keeps a pre-auth key's public prefix and dots out the secret", () => {
    expect(maskSecret("hskey-auth-abc123-s3cr3t")).toBe(`hskey-auth-abc123-${"•".repeat(16)}`);
  });

  it("keeps a few leading characters of a secret without a prefix", () => {
    expect(maskSecret("abcdefghijklmnopqrstuvwxyz")).toBe(`abcdef${"•".repeat(16)}`);
    expect(maskSecret("abc")).toBe(`a${"•".repeat(16)}`);
  });
});

describe(CommandBox, () => {
  it("shows a wrapped command with the line breaks it was written with", async () => {
    const screen = await render(<CommandBox command={docker} wrap />);
    const code = screen.container.querySelector("code");

    expect(code?.textContent).toBe(docker);
    // A word inherits the code's white space, and the last one may break inside.
    await expect
      .element(screen.getByText("tailscale/tailscale"))
      .toHaveStyle({ whiteSpace: "pre-wrap", overflowWrap: "anywhere" });
  });

  it("stays on one line, cut short, when not wrapping", async () => {
    const screen = await render(
      <div style={{ width: 160 }}>
        <CommandBox command="tailscale up --login-server=http://hs.example --authkey=<key>" />
      </div>,
    );
    const code = screen.container.querySelector("code");

    expect(code?.getBoundingClientRect().width).toBeLessThanOrEqual(160);
    expect(code?.getBoundingClientRect().height).toBeLessThan(30);
  });

  // Design spec §13.14: an install command keeps its copy control in view, not only on hover, so a
  // touch screen can copy it too.
  it("shows its copy button without a hover", async () => {
    const screen = await render(<CommandBox command="tailscale up" />);
    const button = screen.getByRole("button", { name: "Copy command" });

    await expect.element(button).toBeVisible();
    expect(getComputedStyle(button.element()).opacity).toBe("1");
    expect(
      Array.from(
        button.element().querySelectorAll("svg"),
        (icon) => getComputedStyle(icon).opacity,
      ),
    ).toStrictEqual(["1"]);
  });
});
