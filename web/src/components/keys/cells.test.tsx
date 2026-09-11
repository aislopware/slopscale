import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { PreAuthKey, User } from "~/api/queries.ts";
import { ExpiryCell } from "~/components/keys/cells.tsx";
import { preAuthKeyColumns } from "~/components/keys/preauth-columns.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";

const millisPerHour = 3_600_000;
const hoursPerDay = 24;
const threeDays = 3;

function inHours(hours: number): string {
  return new Date(Date.now() + hours * millisPerHour).toISOString();
}

describe("the expiry column", () => {
  it("says never for a key that does not expire", async () => {
    const screen = await render(<ExpiryCell value={null} />);

    await expect.element(screen.getByText("Never")).toBeVisible();
  });

  it("marks a key that has run out", async () => {
    const screen = await render(<ExpiryCell value={inHours(-1)} />);

    await expect.element(screen.getByText("Expired")).toBeVisible();
  });

  it("warns about the last day", async () => {
    const screen = await render(<ExpiryCell value={inHours(2)} />);

    await expect.element(screen.getByText("in 2 hours")).toBeVisible();
  });

  it("stays quiet while there is time left", async () => {
    const screen = await render(<ExpiryCell value={inHours(hoursPerDay * threeDays)} />);

    await expect.element(screen.getByText("in 3 days")).toBeVisible();
  });
});

const alice: User = {
  approved: true,
  approvedAt: "2026-01-01T00:00:00Z",
  createdAt: "2026-01-01T00:00:00Z",
  displayName: "Alice Nguyen",
  email: "alice@example.com",
  id: "1",
  name: "alice",
  profilePicUrl: "",
  provider: "local",
  providerId: "",
  role: "admin",
};

const secret = "hskey-auth-69968f8036d70dd6a1b2c3d4e5f6";

const plain: PreAuthKey = {
  aclTags: [],
  groupIds: [],
  createdAt: "2026-01-01T00:00:00Z",
  ephemeral: false,
  expiration: "2027-01-01T00:00:00Z",
  id: "1",
  key: secret,
  preauthorized: true,
  reusable: true,
  used: false,
  user: alice,
};

const tagged: PreAuthKey = { ...plain, aclTags: ["tag:ci", "tag:prod"], id: "2" };

/** The row menu needs query providers; the cells under test do not. */
const cellColumns = preAuthKeyColumns.filter((column) => column.id !== "actions");

function PreAuthTable({ keys }: { readonly keys: readonly PreAuthKey[] }): ReactElement {
  const table = useAppTable({
    data: [...keys],
    columns: cellColumns,
    getRowId: (authKey) => authKey.id,
  });

  return (
    <table.AppTable>
      <DataTable empty={null} />
    </table.AppTable>
  );
}

const previewLength = 24;

describe("the pre-auth key table", () => {
  it("shows enough of the key to tell two apart", async () => {
    const screen = await render(<PreAuthTable keys={[plain]} />);

    await expect.element(screen.getByText(`${secret.slice(0, previewLength)}…`)).toBeVisible();
  });

  it("carries tags in the type cell instead of a column of their own", async () => {
    const screen = await render(<PreAuthTable keys={[tagged]} />);

    await expect
      .element(screen.getByRole("columnheader", { name: "Tags" }))
      .not.toBeInTheDocument();
    await expect.element(screen.getByText("tag:ci")).toBeVisible();
    await expect.element(screen.getByText("tag:prod")).toBeVisible();
    await expect.element(screen.getByText("Reusable")).toBeVisible();
  });

  it("leaves an untagged key with only its type", async () => {
    const screen = await render(<PreAuthTable keys={[plain]} />);

    await expect.element(screen.getByText("Reusable")).toBeVisible();
    await expect.element(screen.getByText("tag:", { exact: false })).not.toBeInTheDocument();
  });
});
