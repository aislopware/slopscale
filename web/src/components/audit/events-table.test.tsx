import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { AuditEvent } from "~/api/queries.ts";
import { EventsTable } from "~/components/audit/events-table.tsx";
import type { EventsTableProps } from "~/components/audit/events-table.tsx";

const deletion: AuditEvent = {
  id: "42",
  createdAt: new Date().toISOString(),
  actorKind: "session",
  actorUserId: "1",
  actorName: "ada",
  action: "node.delete",
  targetKind: "node",
  targetId: "7",
  targetName: "laptop",
  outcome: 200,
  detail: { reason: "retired" },
  remoteAddr: "10.0.0.1",
};

const manyFields: AuditEvent = {
  id: "40",
  createdAt: new Date().toISOString(),
  actorKind: "api_key",
  actorUserId: "",
  actorName: "",
  action: "preauthkey.create",
  targetKind: "preauthkey",
  targetId: "9",
  targetName: "",
  outcome: 200,
  detail: { reusable: true, ephemeral: false, expiration: "2026-09-30T00:00:00Z" },
  remoteAddr: "",
};

const fromTheCli: AuditEvent = {
  id: "41",
  createdAt: new Date().toISOString(),
  actorKind: "local",
  actorUserId: "",
  actorName: "",
  action: "user.create",
  targetKind: "user",
  targetId: "3",
  targetName: "bob",
  outcome: 500,
  detail: {},
  remoteAddr: "10.0.0.2",
};

function noop(): void {
  // The table only reports the click; paging belongs to the page.
}

const base: Omit<EventsTableProps, "events"> = {
  filtered: false,
  onClearFilters: noop,
  hasMore: false,
  loadingMore: false,
  onLoadMore: noop,
};

/**
 * The target cells link into the app, so the table needs a router around it. The padding stands in
 * for the console's sticky `h-12` top bar, which the sticky column header parks under.
 */
function app(props: EventsTableProps): ReactElement {
  const rootRoute = createRootRoute({
    component: () => (
      <div className="pt-12">
        <EventsTable {...props} />
      </div>
    ),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/" });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });

  return <RouterProvider router={router} />;
}

describe(EventsTable, () => {
  it("renders actor, action, target and result", async () => {
    const screen = await render(app({ ...base, events: [deletion, fromTheCli] }));

    await expect.element(screen.getByText("ada")).toBeVisible();
    await expect.element(screen.getByText("Console session")).toBeVisible();
    await expect.element(screen.getByText("node.delete")).toBeVisible();
    await expect.element(screen.getByText("reason=retired")).toBeVisible();
    await expect.element(screen.getByText("Success")).toBeVisible();
    await expect.element(screen.getByText("Failed")).toBeVisible();
  });

  it("reads the target kind as a label and keeps the status out of the cell", async () => {
    const screen = await render(app({ ...base, events: [deletion, manyFields] }));

    await expect.element(screen.getByText("Node")).toBeVisible();
    await expect.element(screen.getByText("Pre-auth key")).toBeVisible();
    await expect.element(screen.getByText("200")).not.toBeInTheDocument();
  });

  it("counts the detail fields past the first two and opens the row to show them", async () => {
    const screen = await render(app({ ...base, events: [manyFields] }));

    await expect
      .element(screen.getByText("expiration=2026-09-30T00:00:00…"))
      .not.toBeInTheDocument();
    await screen.getByRole("button", { name: "+1 more" }).click();

    await expect.element(screen.getByText("expiration")).toBeVisible();
    await expect.element(screen.getByText("2026-09-30T00:00:00Z")).toBeVisible();
  });

  it("names the local socket after the tool that used it", async () => {
    const screen = await render(app({ ...base, events: [fromTheCli] }));

    await expect.element(screen.getByText("CLI")).toBeVisible();
    await expect.element(screen.getByText("unknown")).not.toBeInTheDocument();
  });

  it("links a node target to its machine and a user target to the user list", async () => {
    const screen = await render(app({ ...base, events: [deletion, fromTheCli] }));

    await expect
      .element(screen.getByRole("link", { name: "laptop" }))
      .toHaveAttribute("href", "/machines/7");
    await expect
      .element(screen.getByRole("link", { name: "bob" }))
      .toHaveAttribute("href", "/users?q=bob");
  });

  it("opens a row to show every recorded field, the status and the remote address", async () => {
    const screen = await render(app({ ...base, events: [deletion] }));

    await expect.element(screen.getByText("Remote address")).not.toBeInTheDocument();
    await screen.getByRole("button", { name: "Show details" }).click();

    await expect.element(screen.getByText("Remote address")).toBeVisible();
    await expect.element(screen.getByText("10.0.0.1")).toBeVisible();
    await expect.element(screen.getByText("HTTP status")).toBeVisible();
    await expect.element(screen.getByText("200")).toBeVisible();
  });

  it("shows the empty state and no paging without events", async () => {
    const screen = await render(app({ ...base, events: [], filtered: true }));

    await expect.element(screen.getByText("No events match")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("offers a reset when the filters hid everything", async () => {
    const onClearFilters = vi.fn<() => void>();
    const screen = await render(app({ ...base, events: [], filtered: true, onClearFilters }));

    await screen.getByRole("button", { name: "Clear filters" }).click();
    expect(onClearFilters).toHaveBeenCalledOnce();
  });

  it("loads the next page only when the server has one", async () => {
    const onLoadMore = vi.fn<() => void>();
    const screen = await render(app({ ...base, events: [deletion], hasMore: true, onLoadMore }));

    await screen.getByRole("button", { name: "Load more" }).click();
    expect(onLoadMore).toHaveBeenCalledOnce();
  });
});
