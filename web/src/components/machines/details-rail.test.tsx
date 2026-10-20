import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import type { Node } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { MachineDetails } from "~/components/machines/details-rail.tsx";

const stamp = "2026-01-01T12:00:00Z";

const operator: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scoped: false,
  scopes: [],
  permissions: { "devices:core": true },
};

/** A member with no scopes, looking at someone else's machine. */
const bystander: Me = { ...operator, role: "member", permissions: {} };

const owner = {
  approved: true,
  approvedAt: stamp,
  createdAt: stamp,
  displayName: "Ada",
  email: "",
  id: "1",
  name: "ada",
  profilePicUrl: "",
  provider: "",
  providerId: "",
  role: "member",
};

interface NodeSpec {
  readonly available?: string[];
  readonly approved?: string[];
  readonly served?: string[];
  readonly online?: boolean;
}

function node(id: string, spec: NodeSpec = {}): Node {
  return {
    appConnector: false,
    remoteConfig: false,
    sshServer: false,
    approved: true,
    approvedAt: stamp,
    announcedServices: [],
    approvedRoutes: spec.approved ?? [],
    approvedServices: [],
    availableRoutes: spec.available ?? [],
    clientWarnings: [],
    createdAt: stamp,
    discoKey: "discokey:1",
    expiry: null,
    givenName: `machine-${id}`,
    globalExitNode: false,
    exitNodePriority: 0,
    funnelEnabled: false,
    clientVersion: "",
    os: "",
    osVersion: "",
    updateAvailable: false,
    ephemeral: false,
    id,
    ipAddresses: [],
    lastSeen: stamp,
    machineKey: "mkey:1",
    name: `host-${id}`,
    nodeKey: "nodekey:1",
    online: spec.online ?? true,
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
      user: owner,
    },
    registerMethod: "REGISTER_METHOD_CLI",
    sharedWith: [],
    suspended: false,
    suspendedAt: null,
    subnetRoutes: spec.served ?? [],
    tags: [],
    user: owner,
  };
}

function app(machine: Node, me: Me): ReactElement {
  const rootRoute = createRootRoute({ component: () => <MachineDetails node={machine} me={me} /> });
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(MachineDetails, () => {
  it("refuses a name that cannot be a DNS label before sending it", async () => {
    const screen = await render(app(node("1"), operator));

    await screen.getByRole("button", { name: "Rename machine" }).click();
    const input = screen.getByRole("textbox", { name: "Machine name" });

    await expect.element(input).toHaveFocus();
    await input.fill("not a label");
    await screen.getByRole("button", { name: "Save name" }).click();

    await expect
      .element(screen.getByRole("alert"))
      .toHaveTextContent('contains invalid character " "');
    await expect.element(input).toBeVisible();
  });

  it("puts the name back when the box is emptied or escaped", async () => {
    const screen = await render(app(node("1"), operator));

    await screen.getByRole("button", { name: "Rename machine" }).click();
    await screen.getByRole("textbox", { name: "Machine name" }).fill("");
    await screen.getByRole("button", { name: "Save name" }).click();

    await expect.element(screen.getByText("machine-1")).toBeVisible();

    await screen.getByRole("button", { name: "Rename machine" }).click();
    await userEvent.keyboard("{Escape}");

    await expect
      .element(screen.getByRole("textbox", { name: "Machine name" }))
      .not.toBeInTheDocument();
  });

  it("offers no pencil to a caller who may not rename the machine", async () => {
    const screen = await render(app(node("1"), bystander));

    await expect.element(screen.getByText("machine-1")).toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "Rename machine" }))
      .not.toBeInTheDocument();
    await expect.element(screen.getByRole("button", { name: "Copy machine ID" })).toBeVisible();
  });
});
