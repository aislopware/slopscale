import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Me } from "~/auth/me.ts";
import { CreatedKey } from "~/components/keys/created-key.tsx";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { connectCommand } from "~/components/machines/connect.ts";

/** No users:read, so the dialog asks for a user id instead of loading the user list. */
const operator: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scopes: [],
  permissions: { auth_keys: true, "devices:core": true },
};

describe(connectCommand, () => {
  it("points the machine at this console with the key it was given", () => {
    expect(connectCommand("secret")).toBe(
      `tailscale up --login-server=${globalThis.location.origin} --authkey=secret`,
    );
  });
});

describe(CreatePreAuthKeyDialog, () => {
  it("is titled for the machine, not the key, when adding a machine", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const screen = await render(
      <QueryClientProvider client={client}>
        <CreatePreAuthKeyDialog
          me={operator}
          intent="add-machine"
          open
          onOpenChange={() => {
            // The page owns the dialog.
          }}
        />
      </QueryClientProvider>,
    );

    await expect.element(screen.getByRole("dialog", { name: "Add machine" })).toBeVisible();
  });

  it("keeps the key wording for the keys page", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const screen = await render(
      <QueryClientProvider client={client}>
        <CreatePreAuthKeyDialog
          me={operator}
          open
          onOpenChange={() => {
            // The page owns the dialog.
          }}
        />
      </QueryClientProvider>,
    );

    await expect.element(screen.getByRole("dialog", { name: "Create pre-auth key" })).toBeVisible();
  });
});

describe(CreatedKey, () => {
  it("hands over the command that uses the key", async () => {
    const screen = await render(
      <CreatedKey
        value="secret"
        note="Shown once."
        command={connectCommand("secret")}
        onDone={() => {
          // Nothing to close in a test.
        }}
      />,
    );

    await expect
      .element(
        screen.getByText(
          `tailscale up --login-server=${globalThis.location.origin} --authkey=secret`,
        ),
      )
      .toBeVisible();
    await expect
      .element(
        screen.getByText("Run this on the machine; it appears in the list within a few seconds."),
      )
      .toBeVisible();
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

    await expect
      .element(screen.getByText("Run this on the machine", { exact: false }))
      .not.toBeInTheDocument();
  });
});
