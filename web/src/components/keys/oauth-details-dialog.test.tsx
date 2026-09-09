import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { OAuthClient } from "~/api/queries.ts";
import { OAuthClientDetailsDialog } from "~/components/keys/oauth-details-dialog.tsx";

const federated: OAuthClient = {
  clientId: "f1",
  keyType: "federated",
  description: "CI deploy",
  issuer: "https://token.actions.githubusercontent.com",
  audience: "https://scale.example.com",
  subject: "repo:acme/infrastructure:ref:refs/heads/main",
  customClaimRules: { workflow: "Deploy" },
  scopes: ["devices:core"],
  tags: ["tag:ci"],
  userId: null,
  createdAt: "2026-01-01T12:00:00Z",
};

describe(OAuthClientDetailsDialog, () => {
  // The list truncates the subject to fit its column, and an operator who may read but not change
  // OAuth clients cannot open the edit form; this is where the whole value is.
  it("shows the trust conditions in full, each one copyable", async () => {
    const screen = await render(
      <OAuthClientDetailsDialog
        client={federated}
        open
        onOpenChange={vi.fn<(open: boolean) => void>()}
      />,
    );

    await expect
      .element(screen.getByText("repo:acme/infrastructure:ref:refs/heads/main"))
      .toBeVisible();
    await expect.element(screen.getByText("token.actions.githubusercontent.com")).toBeVisible();
    await expect.element(screen.getByText("Deploy")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Copy subject" })).toBeVisible();
    await expect.element(screen.getByText("Machines")).toBeVisible();
  });
});
