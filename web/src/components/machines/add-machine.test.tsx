import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { CreatedKey } from "~/components/keys/created-key.tsx";
import { connectCommand, joinInstructions, platforms } from "~/components/machines/connect.ts";

describe(connectCommand, () => {
  it("points the machine at this console with the key it was given", () => {
    expect(connectCommand("secret")).toBe(
      `tailscale up --login-server=${globalThis.location.origin} --accept-routes --authkey=secret`,
    );
  });
});

describe(CreatedKey, () => {
  it("hands over the join command per platform with a QR code", async () => {
    const key = "hskey-auth-abc123-s3cr3tv4lu3";
    const masked = `hskey-auth-abc123-${"•".repeat(16)}`;
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    const screen = await render(
      <CreatedKey
        value={key}
        note="Shown once."
        join
        onDone={() => {
          // Nothing to close in a test.
        }}
      />,
    );

    // Design spec §13.15.12: the command shows the key masked and copies it whole.
    await expect
      .element(screen.getByText(`sudo ${connectCommand(masked)}`, { exact: false }))
      .toBeVisible();
    expect(screen.container.querySelector("code")?.textContent).not.toContain("s3cr3t");

    await screen.getByRole("button", { name: "Copy command" }).click();

    expect(writeText).toHaveBeenCalledWith(expect.stringContaining(connectCommand(key)));
    await expect
      .element(screen.getByRole("img", { name: "QR code with the server address" }))
      .not.toBeInTheDocument();

    await screen.getByRole("tab", { name: "Docker" }).click();

    await expect.element(screen.getByText(`TS_AUTHKEY=${masked}`, { exact: false })).toBeVisible();

    await screen.getByRole("tab", { name: "iOS and Android" }).click();

    await expect
      .element(screen.getByRole("img", { name: "QR code with the server address" }))
      .toBeVisible();
    await expect.element(screen.getByText("Copy command")).not.toBeInTheDocument();
  });

  it("shows only the key when a key is what was asked for", async () => {
    const screen = await render(
      <CreatedKey
        value="secret"
        note="Shown once."
        onDone={() => {
          // Nothing to close in a test.
        }}
      />,
    );

    await expect.element(screen.getByRole("tab", { name: "Linux" })).not.toBeInTheDocument();
  });
});

describe(joinInstructions, () => {
  it("puts the key and this server into every shell command", () => {
    for (const platform of platforms.filter((known) => known !== "mobile")) {
      const { command, qr } = joinInstructions(platform, "secret");

      expect(command).toContain("secret");
      expect(command).toContain(globalThis.location.origin);
      expect(qr).toContain("secret");
      expect(qr).not.toContain("\n");
    }
  });

  it("starts the macOS daemon before joining", () => {
    const { command } = joinInstructions("macos", "secret");

    expect(command).toMatch(/brew services start tailscale .*tailscale up/u);
  });

  it("points the Docker container's state at the mounted volume", () => {
    const { command } = joinInstructions("docker", "secret");

    expect(command).toContain("-v tailscale-state:/var/lib/tailscale");
    expect(command).toContain("TS_STATE_DIR=/var/lib/tailscale");
  });

  it("hands the phone apps the server address instead of a command", () => {
    const { command, qr } = joinInstructions("mobile", "secret");

    expect(command).toBeNull();
    expect(qr).toBe(globalThis.location.origin);
  });
});
