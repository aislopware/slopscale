import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { Group } from "~/api/queries.ts";
import { GroupEditor } from "~/components/access/group-editor.tsx";

// The typed client keeps the fetch it found when it was made, so the stand-in goes in before any
// module loads; every answer is an empty success.
const server = vi.hoisted(() => {
  const state: { requests: { method: string; url: string; body: unknown }[] } = { requests: [] };

  globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = new Request(input, init);
    const text = await request.clone().text();

    state.requests.push({
      method: request.method,
      url: new URL(request.url).pathname,
      body: text === "" ? undefined : JSON.parse(text),
    });

    return Response.json({});
  };

  return state;
});

const stamp = "2026-01-01T12:00:00Z";
const hour = 3600;

const exitNodeAccess: Group = {
  autoApprove: true,
  builtin: "",
  source: "",
  createdAt: stamp,
  description: "",
  expiries: [],
  id: "7",
  maxRequestSeconds: hour,
  name: "Exit node access",
  nodeIds: [],
  requestable: true,
  updatedAt: stamp,
  userIds: [],
};

/** What the page asked the server to change; refetches after a save are only reads. */
function sent(): typeof server.requests {
  return server.requests.filter((request) => request.method !== "GET");
}

function app(group?: Group): ReactElement {
  server.requests = [];
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute();
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => <GroupEditor group={group} rules={[]} nodes={[]} users={[]} canEdit />,
      }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/policy/groups",
        component: () => <p>Groups</p>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });

  return (
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(GroupEditor, () => {
  it("creates a self-service group that grants an hour at most", async () => {
    const screen = await render(app());

    await expect
      .element(screen.getByRole("switch", { name: "Approve requests automatically" }))
      .not.toBeInTheDocument();
    await screen.getByRole("textbox", { name: /Group name/v }).fill("Exit node access");
    await screen.getByRole("switch", { name: "Users may request access" }).click();
    await screen.getByRole("switch", { name: "Approve requests automatically" }).click();
    await screen.getByRole("combobox", { name: "Longest request" }).click();
    await screen.getByRole("option", { name: "1 hour" }).click();
    await screen.getByRole("button", { name: "Create group" }).click();

    await expect.element(screen.getByText("Groups")).toBeVisible();
    expect(sent()).toStrictEqual([
      {
        method: "POST",
        url: "/api/v1/group",
        body: {
          name: "Exit node access",
          description: "",
          nodeIds: [],
          userIds: [],
          requestable: true,
          autoApprove: true,
          maxRequestSeconds: hour,
        },
      },
    ]);
  });

  it("stops approving by itself when requests are turned off, and keeps the cap", async () => {
    const screen = await render(app(exitNodeAccess));

    await expect
      .element(screen.getByRole("combobox", { name: "Longest request" }))
      .toHaveTextContent("1 hour");
    await expect
      .element(screen.getByRole("switch", { name: "Approve requests automatically" }))
      .toBeChecked();
    await screen.getByRole("switch", { name: "Users may request access" }).click();
    await expect
      .element(screen.getByRole("switch", { name: "Approve requests automatically" }))
      .not.toBeInTheDocument();
    await screen.getByRole("button", { name: "Save group" }).click();

    await expect.element(screen.getByText("Groups")).toBeVisible();
    expect(sent()).toStrictEqual([
      {
        method: "PATCH",
        url: "/api/v1/group/7",
        body: {
          name: "Exit node access",
          description: "",
          nodeIds: [],
          userIds: [],
          requestable: false,
          autoApprove: false,
          maxRequestSeconds: hour,
        },
      },
    ]);
  });
});
