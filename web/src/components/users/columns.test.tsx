import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { User } from "~/api/queries.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { columns } from "~/components/users/columns.tsx";

const alice: User = {
  approved: true,
  approvedAt: "2026-01-01T00:00:00Z",
  createdAt: "2026-01-01T00:00:00Z",
  displayName: "Alice Nguyen",
  email: "alice@example.com",
  id: "1",
  name: "alice",
  profilePicUrl: "",
  provider: "oidc",
  providerId: "alice",
  role: "admin",
};

const bob: User = {
  ...alice,
  displayName: "Bob Tran",
  email: "bob@example.com",
  id: "2",
  name: "bob",
  provider: "local",
  role: "member",
};

/** The row menu needs query and router providers; the cells under test do not. */
const cellColumns = columns.filter((column) => column.id !== "actions");

function UsersTable({ users }: { readonly users: readonly User[] }): ReactElement {
  const table = useAppTable({
    data: [...users],
    columns: cellColumns,
    getRowId: (user) => user.id,
  });

  return (
    <table.AppTable>
      <DataTable empty={null} />
    </table.AppTable>
  );
}

describe("the users table", () => {
  it("has no provider column", async () => {
    const screen = await render(<UsersTable users={[alice, bob]} />);

    await expect.element(screen.getByRole("button", { name: "Joined" })).toBeInTheDocument();
    await expect.element(screen.getByText("Provider")).not.toBeInTheDocument();
  });

  it("names an external provider under the email", async () => {
    const screen = await render(<UsersTable users={[alice]} />);

    await expect.element(screen.getByText("alice@example.com")).toBeVisible();
    await expect.element(screen.getByText("OpenID Connect")).toBeVisible();
  });

  it("leaves a local user's email alone", async () => {
    const screen = await render(<UsersTable users={[bob]} />);

    await expect.element(screen.getByText("bob@example.com")).toBeVisible();
    await expect.element(screen.getByText("Local")).not.toBeInTheDocument();
  });
});

describe("a user without a display name", () => {
  const dev: User = { ...alice, displayName: "", email: "", name: "dev", provider: "" };

  it("names the account once, rather than repeating it under itself", async () => {
    const screen = await render(<UsersTable users={[dev]} />);

    await expect.element(screen.getByText("dev", { exact: true })).toBeVisible();
    expect(screen.getByText("dev", { exact: true }).elements()).toHaveLength(1);
  });

  it("leaves a dash where there is no email", async () => {
    const screen = await render(<UsersTable users={[dev]} />);

    await expect.element(screen.getByText("—")).toBeVisible();
  });
});
