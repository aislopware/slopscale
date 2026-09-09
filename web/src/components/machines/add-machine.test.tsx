import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Me } from "~/auth/me.ts";
import { CreatedKey } from "~/components/keys/created-key.tsx";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { connectCommand, joinInstructions, platforms } from "~/components/machines/connect.ts";

/** No users:read, so the dialog asks for a user id instead of loading the user list. */
const operator: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scopes: [],
  permissions: { auth_keys: true, "devices:core": true },
};

/** The dialog loads users and groups when allowed, so it needs a query client around it. */
function queried(element: ReactElement): ReactElement {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return <QueryClientProvider client={client}>{element}</QueryClientProvider>;
}

describe(connectCommand, () => {
  it("points the machine at this console with the key it was given", () => {
    expect(connectCommand("secret")).toBe(
      `tailscale up --login-server=${globalThis.location.origin} --authkey=secret`,
    );
  });
});

describe(CreatePreAuthKeyDialog, () => {
  it("is titled for the machine, not the key, when adding a machine", async () => {
    const screen = await render(
      queried(
        <CreatePreAuthKeyDialog
          me={operator}
          intent="add-machine"
          open
          onOpenChange={() => {
            // The page owns the dialog.
          }}
        />,
      ),
    );

    await expect.element(screen.getByRole("dialog", { name: "Add machine" })).toBeVisible();
  });

  it("keeps the key wording for the keys page", async () => {
    const screen = await render(
      queried(
        <CreatePreAuthKeyDialog
          me={operator}
          open
          onOpenChange={() => {
            // The page owns the dialog.
          }}
        />,
      ),
    );

    await expect.element(screen.getByRole("dialog", { name: "Create pre-auth key" })).toBeVisible();
  });
});

describe(CreatedKey, () => {
  it("hands over the join command per platform with a QR code", async () => {
    const screen = await render(
      <CreatedKey
        value="secret"
        note="Shown once."
        join
        onDone={() => {
          // Nothing to close in a test.
        }}
      />,
    );

    await expect
      .element(screen.getByText(`sudo ${connectCommand("secret")}`, { exact: false }))
      .toBeVisible();
    await expect
      .element(screen.getByRole("img", { name: "QR code with the server address" }))
      .not.toBeInTheDocument();

    await screen.getByRole("tab", { name: "Docker" }).click();

    await expect.element(screen.getByText("TS_AUTHKEY=secret", { exact: false })).toBeVisible();

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
