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

import type { Me } from "~/auth/me.ts";
import { InvitePage } from "~/components/users/invite-page.tsx";
import { AddUserPage } from "~/components/users/user-page.tsx";

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

const operator: Me = {
  kind: "api_key",
  role: "admin",
  allAccess: true,
  scoped: false,
  scopes: [],
  permissions: { all: true, users: true, "users:read": true },
};

type Answer = (request: Request) => Response;

const answerEmpty: Answer = (): Response => Response.json({});

function refuse(status: number, detail: string): Answer {
  return (): Response =>
    Response.json(
      { title: "Refused", status, detail },
      { status, headers: { "content-type": "application/problem+json" } },
    );
}

/** What the page asked the server to change; refetches after a save are only reads. */
function sent(): typeof server.requests {
  return server.requests.filter((request) => request.method !== "GET");
}

/** The invitation as the server answers it when its mail could not go out. */
function created(): Response {
  return Response.json({
    invite: { email: "bob@example.com" },
    url: "https://vpn.example.com/invite/abc",
    emailSent: false,
    emailError: "smtp: connection refused",
  });
}

function app(page: () => ReactElement, answer: Answer = answerEmpty): ReactElement {
  server.requests = [];
  server.answer = answer;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute();
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: page }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/users",
        component: () => <p>Users list</p>,
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

describe(AddUserPage, () => {
  it("asks for the username before sending anything", async () => {
    const screen = await render(app(() => <AddUserPage canEdit />));

    await screen.getByRole("button", { name: "Add user" }).click();

    await expect.element(screen.getByText("Give the user a username.")).toBeVisible();
    await expect.element(screen.getByLabelText("Username")).toHaveAttribute("aria-invalid", "true");
    expect(sent()).toStrictEqual([]);
  });

  it("says what is wrong with the address once the field is left", async () => {
    const screen = await render(app(() => <AddUserPage canEdit />));

    await screen.getByLabelText(/^Email/v).fill("alice");
    await screen.getByLabelText(/^Display name/v).click();

    await expect
      .element(screen.getByText("Use an address such as alice@example.com."))
      .toBeVisible();
  });

  it("creates the user with what was filled in and goes back to the list", async () => {
    const screen = await render(app(() => <AddUserPage canEdit />));

    await screen.getByLabelText("Username").fill(" alice ");
    await screen.getByLabelText(/^Email/v).fill("alice@example.com");
    await screen.getByRole("button", { name: "Add user" }).click();

    await expect.element(screen.getByText("Users list")).toBeVisible();
    expect(sent()).toStrictEqual([
      { method: "POST", url: "/api/v1/user", body: { name: "alice", email: "alice@example.com" } },
    ]);
  });

  it("keeps the form and shows the server's refusal in a banner", async () => {
    const screen = await render(
      app(() => <AddUserPage canEdit />, refuse(409, "a user named alice already exists")),
    );

    await screen.getByLabelText("Username").fill("alice");
    await screen.getByRole("button", { name: "Add user" }).click();

    await expect.element(screen.getByText("The user was not created")).toBeVisible();
    await expect.element(screen.getByText(/a user named alice already exists/v)).toBeVisible();
    await expect.element(screen.getByLabelText("Username")).toHaveValue("alice");
  });
});

describe(InvitePage, () => {
  it("asks for the address before sending anything", async () => {
    const screen = await render(app(() => <InvitePage me={operator} />));

    await screen.getByRole("button", { name: "Send invitation" }).click();

    await expect.element(screen.getByText("Enter the address to invite.")).toBeVisible();
    expect(sent()).toStrictEqual([]);
  });

  it("sums up the invitation as it is filled in", async () => {
    const screen = await render(app(() => <InvitePage me={operator} />));

    await screen.getByLabelText(/^Email/v).fill("bob@example.com");
    await screen.getByRole("radio", { name: /^Auditor/v }).click();
    await screen.getByRole("radio", { name: "30 days" }).click();

    await expect
      .element(screen.getByText(/joins as/v))
      .toHaveTextContent("bob@example.com joins as Auditor. The link works for 30 days.");
  });

  it("sends the invitation and shows its link in place, with why the email did not go", async () => {
    const screen = await render(app(() => <InvitePage me={operator} />, created));

    await screen.getByLabelText(/^Email/v).fill("bob@example.com");
    await screen.getByRole("radio", { name: /^Network admin/v }).click();
    await screen.getByRole("button", { name: "Send invitation" }).click();

    await expect.element(screen.getByRole("heading", { name: "Invitation created" })).toBeVisible();
    await expect.element(screen.getByText("https://vpn.example.com/invite/abc")).toBeVisible();
    await expect
      .element(screen.getByText("Smtp: connection refused. Send the link yourself."))
      .toBeVisible();
    expect(sent()).toStrictEqual([
      {
        method: "POST",
        url: "/api/v1/invite",
        body: { email: "bob@example.com", role: "network-admin", expiry: "168h" },
      },
    ]);
  });

  it("keeps the form and shows the server's refusal in a banner", async () => {
    const screen = await render(
      app(
        () => <InvitePage me={operator} />,
        refuse(409, "an invitation for bob@example.com is pending"),
      ),
    );

    await screen.getByLabelText(/^Email/v).fill("bob@example.com");
    await screen.getByRole("button", { name: "Send invitation" }).click();

    await expect.element(screen.getByText("The invitation was not sent")).toBeVisible();
    await expect
      .element(screen.getByText(/an invitation for bob@example.com is pending/v))
      .toBeVisible();
  });
});
