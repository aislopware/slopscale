import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { AuditEvent } from "~/api/queries.ts";
import { EventsTable } from "~/components/audit/events-table.tsx";

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

const failedLogin: AuditEvent = {
  id: "41",
  createdAt: new Date().toISOString(),
  actorKind: "system",
  actorUserId: "",
  actorName: "headscale",
  action: "console.login",
  targetKind: "",
  targetId: "",
  targetName: "",
  outcome: 500,
  detail: {},
  remoteAddr: "10.0.0.2",
};

function noop(): void {
  // The table only reports the click; paging belongs to the page.
}

describe(EventsTable, () => {
  it("renders actor, action, target and result", async () => {
    const screen = await render(
      <EventsTable
        events={[deletion, failedLogin]}
        filtered={false}
        hasMore={false}
        loadingMore={false}
        onLoadMore={noop}
      />,
    );

    await expect.element(screen.getByText("ada")).toBeVisible();
    await expect.element(screen.getByText("session")).toBeVisible();
    await expect.element(screen.getByText("node.delete")).toBeVisible();
    await expect.element(screen.getByText("laptop")).toBeVisible();
    await expect.element(screen.getByText("reason=retired")).toBeVisible();
    await expect.element(screen.getByText("200")).toBeVisible();
    await expect.element(screen.getByText("500")).toBeVisible();
    await expect.element(screen.getByText("headscale")).toBeVisible();
  });

  it("shows the empty state and no paging without events", async () => {
    const screen = await render(
      <EventsTable events={[]} filtered hasMore={false} loadingMore={false} onLoadMore={noop} />,
    );

    await expect.element(screen.getByText("No events match")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("loads the next page only when the server has one", async () => {
    const onLoadMore = vi.fn<() => void>();
    const screen = await render(
      <EventsTable
        events={[deletion]}
        filtered={false}
        hasMore
        loadingMore={false}
        onLoadMore={onLoadMore}
      />,
    );

    await screen.getByRole("button", { name: "Load more" }).click();
    expect(onLoadMore).toHaveBeenCalledOnce();
  });
});
