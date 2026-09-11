import { describe, expect, it } from "vitest";

import type { Node, User } from "~/api/queries.ts";
import type { NodeClientUpdateResult } from "~/api/schema.gen.ts";
import {
  clientUpdatePlanSummary,
  clientUpdateSummary,
  planClientUpdates,
  summariseClientUpdates,
} from "~/components/machines/bulk.ts";
import { visibleSelection } from "~/components/machines/selection.tsx";
import { nodeName } from "~/lib/node.ts";

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

describe(planClientUpdates, () => {
  it("keeps only the ticked machines that are connected and behind", () => {
    expect(planClientUpdates(everything, new Set(["1", "2", "3"]))).toStrictEqual({
      eligible: ["1"],
      offline: 1,
      current: 1,
    });
  });

  it("ignores a machine that is not ticked", () => {
    expect(planClientUpdates(everything, new Set(["2"]))).toStrictEqual({
      eligible: [],
      offline: 0,
      current: 1,
    });
  });
});

describe(clientUpdatePlanSummary, () => {
  it("says how many will be asked when nothing is stepped over", () => {
    expect(clientUpdatePlanSummary({ eligible: ["1", "2"], offline: 0, current: 0 })).toBe(
      "2 machines will be asked to update.",
    );
  });

  it("names what it is stepping over, so a run is no surprise", () => {
    expect(clientUpdatePlanSummary({ eligible: ["1"], offline: 2, current: 3 })).toBe(
      "1 machine will be asked to update. Skipping 2 offline and 3 already current.",
    );
  });
});

/**
 * The search box narrows the table without narrowing the collection behind it, so what a bulk
 * action reaches has to come from the rows the table would show. These replay that: the ids are the
 * table's filtered model, and the selection is what the page holds between renders.
 */
describe("a selection under a search", () => {
  const matching = (query: string): string[] =>
    everything
      .filter((candidate) => nodeName(candidate).includes(query))
      .map((candidate) => candidate.id);

  it("selects every match and nothing behind the search", () => {
    const ids = matching("machine-1");
    const chosen = new Set(ids);
    const selected = visibleSelection(ids, chosen);

    expect([...selected]).toStrictEqual(["1"]);
    expect(planClientUpdates(everything, selected).eligible).toStrictEqual(["1"]);
  });

  it("drops the ids a narrower search hides instead of acting on them", () => {
    const chosen = new Set(matching("machine-"));

    expect([...chosen]).toStrictEqual(["1", "2", "3"]);

    const narrowed = visibleSelection(matching("machine-3"), chosen);

    expect([...narrowed]).toStrictEqual(["3"]);
    // The one machine still showing is offline, so an update would reach nobody.
    expect(planClientUpdates(everything, narrowed)).toStrictEqual({
      eligible: [],
      offline: 1,
      current: 0,
    });
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
