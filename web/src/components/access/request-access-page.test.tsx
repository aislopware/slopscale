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

import type { AccessRequestOptions } from "~/api/queries.ts";
import { RequestAccessForm } from "~/components/access/request-access-page.tsx";

// The typed client keeps the fetch it found when it was made, so the stand-in goes in before any
// module loads; each test says how the server answers.
const server = vi.hoisted(() => {
  const state: {
    requests: { method: string; url: string; body: unknown }[];
    answer: (request: Request) => Response;
  } = {
    requests: [],
    answer: () => Response.json({}),
  };

  globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = new Request(input, init);
    const text = await request.clone().text();

    state.requests.push({
      method: request.method,
      url: new URL(request.url).pathname,
      body: text === "" ? undefined : JSON.parse(text),
    });

    return state.answer(request);
  };

  return state;
});

const options: AccessRequestOptions = {
  groups: [
    { id: "3", name: "Production", description: "Databases and the deploy hosts." },
    { id: "4", name: "Staging" },
  ],
  nodes: [{ id: "9", name: "ada-laptop" }],
};

/** What the page asked the server to change; refetches after a save are only reads. */
function sent(): typeof server.requests {
  return server.requests.filter((request) => request.method !== "GET");
}

type Answer = (request: Request) => Response;

function refuse(): Response {
  return Response.json(
    { title: "Conflict", status: 409, detail: "you already have a pending request" },
    { status: 409, headers: { "content-type": "application/problem+json" } },
  );
}

function app(answer: Answer = (): Response => Response.json({})): ReactElement {
  server.requests = [];
  server.answer = answer;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute();
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => <RequestAccessForm options={options} />,
      }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/access",
        component: () => <p>My access</p>,
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

describe(RequestAccessForm, () => {
  it("sends the request for the chosen time and goes back to My access", async () => {
    const screen = await render(app());

    await expect.element(screen.getByText("Databases and the deploy hosts.")).toBeVisible();
    await screen.getByRole("radio", { name: "4 hours" }).click();
    await screen.getByRole("textbox", { name: "Reason" }).fill("  Hotfix for ticket 42 ");
    await expect
      .element(screen.getByText(/once an approver agrees/v))
      .toHaveTextContent(
        "Every machine you own joins Production for 4 hours once an approver agrees.",
      );
    await screen.getByRole("button", { name: "Send request" }).click();

    await expect.element(screen.getByText("My access")).toBeVisible();
    expect(sent()).toStrictEqual([
      {
        method: "POST",
        url: "/api/v1/access-request",
        body: { groupId: "3", durationSeconds: 14_400, reason: "Hotfix for ticket 42" },
      },
    ]);
  });

  it("keeps the form and shows the server's refusal in a banner", async () => {
    const screen = await render(app(refuse));

    await screen.getByRole("button", { name: "Send request" }).click();

    await expect.element(screen.getByText("The request was not sent")).toBeVisible();
    await expect.element(screen.getByText(/you already have a pending request/v)).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Send request" })).toBeVisible();
  });
});
