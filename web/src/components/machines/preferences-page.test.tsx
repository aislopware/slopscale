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

import { nodePreferencesQuery } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import type { NodePreferences } from "~/api/schema.gen.ts";
import { MachinePreferences } from "~/components/machines/preferences-page.tsx";

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

const stamp = "2026-01-01T12:00:00Z";

const ada: User = {
  approved: true,
  approvedAt: stamp,
  createdAt: stamp,
  displayName: "Ada",
  email: "ada@example.com",
  id: "1",
  name: "ada",
  profilePicUrl: "",
  provider: "",
  providerId: "",
  role: "member",
};

function machine(overrides: Partial<Node> = {}): Node {
  return {
    appConnector: false,
    remoteConfig: true,
    sshServer: false,
    approved: true,
    approvedAt: stamp,
    announcedServices: [],
    approvedRoutes: [],
    approvedServices: [],
    availableRoutes: [],
    clientWarnings: [],
    createdAt: stamp,
    discoKey: "discokey:1",
    expiry: null,
    givenName: "office-minipc",
    globalExitNode: false,
    exitNodePriority: 0,
    funnelEnabled: false,
    clientVersion: "1.86.0",
    os: "",
    osVersion: "",
    updateAvailable: false,
    ephemeral: false,
    id: "7",
    ipAddresses: [],
    lastSeen: stamp,
    machineKey: "mkey:1",
    name: "office-minipc",
    nodeKey: "nodekey:1",
    online: true,
    preAuthKey: {
      aclTags: [],
      createdAt: null,
      ephemeral: false,
      expiration: null,
      id: "0",
      key: "",
      preauthorized: false,
      reusable: false,
      used: false,
      user: ada,
    },
    registerMethod: "REGISTER_METHOD_CLI",
    sharedWith: [],
    subnetRoutes: [],
    suspended: false,
    suspendedAt: null,
    tags: [],
    user: ada,
    ...overrides,
  };
}

const preferences: NodePreferences = {
  acceptDns: true,
  acceptRoutes: false,
  advertiseConnector: false,
  advertiseExitNode: false,
  advertiseRoutes: ["10.0.0.0/24"],
  autoUpdateApply: false,
  autoUpdateCheck: true,
  exitNode: "",
  exitNodeAllowLanAccess: false,
  hostname: "",
  postureChecking: false,
  runSsh: false,
  shieldsUp: false,
};

type Answer = (request: Request) => Response;

const answerPreferences: Answer = (): Response => Response.json({ preferences });

function refuse(): Response {
  return Response.json(
    { title: "Bad Gateway", status: 502, detail: "the client refused the change" },
    { status: 502, headers: { "content-type": "application/problem+json" } },
  );
}

function app(node: Node, answer: Answer = answerPreferences): ReactElement {
  server.requests = [];
  server.answer = answer;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  client.setQueryData(nodePreferencesQuery(node.id).queryKey, preferences);

  const rootRoute = createRootRoute();
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => <MachinePreferences node={node} canEdit />,
      }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/machines/$nodeId/settings",
        component: () => <p>Settings tab</p>,
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

/** What the page asked the server to change; the refetches after a save are only reads. */
function sent(): typeof server.requests {
  return server.requests.filter((request) => request.method !== "GET");
}

describe(MachinePreferences, () => {
  it("says the machine must be online before it can be asked", async () => {
    const screen = await render(app(machine({ online: false })));

    await expect.element(screen.getByText("The machine is offline")).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Back to the machine" })).toBeVisible();
    expect(screen.container.querySelector("form")).toBeNull();
  });

  it("gives the command to run when the machine keeps its preferences to itself", async () => {
    const screen = await render(app(machine({ remoteConfig: false })));

    await expect
      .element(screen.getByText("The tailnet cannot change this machine's preferences"))
      .toBeVisible();
    await expect.element(screen.getByText("tailscale set --remote-config")).toBeVisible();
  });

  it("sends only the switch that changed, then goes back to the machine", async () => {
    const screen = await render(app(machine()));

    await screen.getByRole("switch", { name: "Shields up" }).click();
    await screen.getByRole("button", { name: "Save preferences" }).click();

    await expect.element(screen.getByText("Settings tab")).toBeVisible();
    expect(sent()).toStrictEqual([
      { method: "PATCH", url: "/api/v1/node/7/preferences", body: { shieldsUp: true } },
    ]);
  });

  it("stays on a wrong route and sends nothing", async () => {
    const screen = await render(app(machine()));

    await screen
      .getByRole("textbox", { name: /Advertised routes/v })
      .first()
      .fill("10.0.0.300/24");
    await screen.getByRole("button", { name: "Save preferences" }).click();

    await expect.element(screen.getByRole("button", { name: "Save preferences" })).toBeVisible();
    expect(sent()).toStrictEqual([]);
  });

  it("shows the client's refusal in a banner and keeps the edit", async () => {
    const screen = await render(app(machine(), refuse));

    await screen.getByRole("switch", { name: "Run SSH" }).click();
    await screen.getByRole("button", { name: "Save preferences" }).click();

    await expect.element(screen.getByText("The machine did not take the change")).toBeVisible();
    await expect.element(screen.getByText("the client refused the change")).toBeVisible();
    await expect.element(screen.getByRole("switch", { name: "Run SSH" })).toBeChecked();
  });
});
