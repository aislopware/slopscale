import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Node } from "~/api/queries.ts";
import { signInCommand } from "~/components/machines/connect.ts";
import { ConnectivitySection } from "~/components/machines/connectivity.tsx";

const stamp = "2026-01-01T12:00:00Z";

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

function node(spec: { readonly online: boolean; readonly expiry?: string }): Node {
  return {
    appConnector: false,
    remoteConfig: false,
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
    expiry: spec.expiry ?? null,
    givenName: "laptop",
    globalExitNode: false,
    exitNodePriority: 0,
    funnelEnabled: false,
    clientVersion: "",
    os: "",
    osVersion: "",
    updateAvailable: false,
    ephemeral: false,
    id: "1",
    ipAddresses: [],
    lastSeen: stamp,
    machineKey: "mkey:1",
    name: "laptop",
    nodeKey: "nodekey:1",
    online: spec.online,
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
    subnetRoutes: [],
    tags: [],
    user: owner,
  };
}

function card(machine: Node): ReactElement {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={client}>
      <ConnectivitySection node={machine} />
    </QueryClientProvider>
  );
}

// Design spec §13.15.5: the Mesh node page puts the way to connect in this card while the node is
// offline, rather than a note that there is no telemetry.
describe(ConnectivitySection, () => {
  it("tells a disconnected machine how to reconnect, with the command to copy", async () => {
    const screen = await render(card(node({ online: false })));

    await expect.element(screen.getByRole("heading", { name: "Reconnect laptop" })).toBeVisible();
    expect(screen.container.querySelector("code")?.textContent).toBe(signInCommand());
    await expect.element(screen.getByRole("button", { name: "Copy command" })).toBeVisible();
    await expect.element(screen.getByText("No network report yet")).not.toBeInTheDocument();
  });

  it("signs a machine with an expired key in again", async () => {
    const screen = await render(card(node({ online: false, expiry: stamp })));

    await expect
      .element(screen.getByRole("heading", { name: "Sign laptop in again" }))
      .toBeVisible();
    expect(screen.container.querySelector("code")?.textContent).toBe(
      `${signInCommand()} --force-reauth`,
    );
  });

  it("waits for the report of a connected machine without a reconnect step", async () => {
    const screen = await render(card(node({ online: true })));

    await expect.element(screen.getByText("No network report yet")).toBeVisible();
    await expect
      .element(screen.getByRole("heading", { name: /Reconnect/u }))
      .not.toBeInTheDocument();
  });
});
