import { describe, expect, it } from "vitest";

import type { Node, User } from "~/api/queries.ts";
import type { NodeClientUpdateResult } from "~/api/schema.gen.ts";
import {
  clientUpdateSummary,
  outdatedSelection,
  summariseClientUpdates,
} from "~/components/machines/bulk.ts";

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

function node(id: string, overrides: Partial<Node> = {}): Node {
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
    expiry: null,
    givenName: `machine-${id}`,
    globalExitNode: false,
    funnelEnabled: false,
    clientVersion: "1.86.0",
    updateAvailable: false,
    ephemeral: false,
    id,
    ipAddresses: [],
    lastSeen: stamp,
    machineKey: "mkey:1",
    name: `host-${id}`,
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

const behind = node("1", { updateAvailable: true });
const current = node("2");
const offline = node("3", { updateAvailable: true, online: false });
const everything = [behind, current, offline];

describe(outdatedSelection, () => {
  it("keeps only the ticked machines that are connected and behind", () => {
    expect(outdatedSelection(everything, new Set(["1", "2", "3"]))).toStrictEqual(["1"]);
  });

  it("ignores a machine that is not ticked", () => {
    expect(outdatedSelection(everything, new Set(["2"]))).toStrictEqual([]);
  });
});

describe(summariseClientUpdates, () => {
  const results: NodeClientUpdateResult[] = [
    { nodeId: "1", started: true },
    { nodeId: "3", started: false, error: "auto-update is not allowed" },
    { nodeId: "42", started: false },
  ];

  it("splits the answers and names the machines that refused", () => {
    const outcome = summariseClientUpdates(results, everything);

    expect(outcome.started).toBe(1);
    expect(outcome.refused).toStrictEqual([
      { nodeId: "3", name: "machine-3", message: "auto-update is not allowed" },
      { nodeId: "42", name: "#42", message: "The client gave no reason." },
    ]);
  });
});

describe(clientUpdateSummary, () => {
  it("says how many started when every client took it on", () => {
    expect(clientUpdateSummary({ started: 3, refused: [] })).toBe("Started on 3 machines");
    expect(clientUpdateSummary({ started: 1, refused: [] })).toBe("Started on 1 machine");
  });

  it("reports both sides of a mixed run", () => {
    const refused = [{ nodeId: "3", name: "laptop", message: "no" }];

    expect(clientUpdateSummary({ started: 2, refused })).toBe("Started on 2, 1 refused");
  });

  it("leaves the zero out when nothing started", () => {
    const refused = [
      { nodeId: "3", name: "laptop", message: "no" },
      { nodeId: "4", name: "desk", message: "no" },
    ];

    expect(clientUpdateSummary({ started: 0, refused })).toBe("2 machines refused");
  });
});
