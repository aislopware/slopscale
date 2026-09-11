import { describe, expect, it } from "vitest";

import type { Node, Service, ServiceHost } from "~/api/queries.ts";
import {
  approvedServicesOf,
  hostedServices,
  machineServices,
  portError,
  serviceNameIssue,
  serviceReach,
  toServiceRows,
  withServiceApproved,
} from "~/components/services/model.ts";

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

function node(
  id: string,
  announced: readonly { name: string; ports: string[]; active: boolean }[],
  approved: string[],
): Node {
  return {
    announcedServices: [...announced],
    appConnector: false,
    remoteConfig: false,
    sshServer: false,
    approved: true,
    approvedAt: stamp,
    approvedRoutes: [],
    approvedServices: approved,
    availableRoutes: [],
    clientVersion: "",
    os: "",
    osVersion: "",
    clientWarnings: [],
    createdAt: stamp,
    discoKey: "discokey:1",
    ephemeral: false,
    expiry: null,
    funnelEnabled: false,
    givenName: `machine-${id}`,
    globalExitNode: false,
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
      user: owner,
    },
    registerMethod: "REGISTER_METHOD_CLI",
    sharedWith: [],
    subnetRoutes: [],
    suspended: false,
    suspendedAt: null,
    tags: ["tag:web"],
    updateAvailable: false,
    user: owner,
  };
}

function host(nodeId: string, standing: Partial<ServiceHost> = {}): ServiceHost {
  return {
    active: false,
    announced: false,
    approved: false,
    name: `machine-${nodeId}`,
    nodeId,
    ports: [],
    primary: false,
    ...standing,
  };
}

function service(name: string, hosts: ServiceHost[] = []): Service {
  return {
    addresses: ["100.80.0.1", "fd7a::1"],
    comment: "",
    createdAt: stamp,
    displayName: "",
    dnsName: `${name.replace("svc:", "")}.example.ts.net`,
    hosts,
    id: name,
    name,
    ports: [],
    updatedAt: stamp,
  };
}

describe(serviceNameIssue, () => {
  it.each(["web", "web-2", "a".repeat(63)])("accepts %s", (label) => {
    expect(serviceNameIssue(label)).toBeNull();
  });

  it("rejects what a DNS label cannot hold", () => {
    expect(serviceNameIssue("")).toBe("empty DNS label");
    expect(serviceNameIssue("-web")).toBe("must start with a letter or number");
    expect(serviceNameIssue("web-")).toBe("must end with a letter or number");
    expect(serviceNameIssue("web_one")).toBe('contains invalid character "_"');
    expect(serviceNameIssue("a".repeat(64))).toBe("DNS label is longer than 63 characters");
  });

  it("rejects an upper-case label, which the server would quietly lower case", () => {
    expect(serviceNameIssue("Web")).toBe("must be lower case");
  });
});

describe(portError, () => {
  it.each(["tcp:443", "udp:53-60", "tcp:*", "*:443", "sctp:1-65535"])("accepts %s", (value) => {
    expect(portError(value)).toBeNull();
  });

  it.each(["443", "http:443", "tcp:", "tcp:70000", "tcp:1-2-3", "tcp:60-53"])(
    "rejects %s",
    (value) => {
      expect(portError(value)).not.toBeNull();
    },
  );
});

describe(serviceReach, () => {
  it("names the step that is missing", () => {
    const serving = host("1", { announced: true, active: true, approved: true });

    expect(serviceReach(service("svc:web", [serving]))).toBe("reachable");
    expect(serviceReach(service("svc:web", [{ ...serving, approved: false }]))).toBe("unapproved");
    expect(serviceReach(service("svc:web", [{ ...serving, active: false }]))).toBe("inactive");
    expect(serviceReach(service("svc:web"))).toBe("unhosted");
  });
});

describe(toServiceRows, () => {
  it("spells out the label, the state and who serves it", () => {
    const rows = toServiceRows([
      service("svc:web", [
        host("1", { announced: true, active: true, approved: true, primary: true }),
        host("2", { announced: true, active: true }),
      ]),
    ]);

    expect(rows.map((row) => [row.label, row.reach, row.primaryHost, row.hostNames])).toStrictEqual(
      [["web", "reachable", "machine-1", "machine-1, machine-2"]],
    );
  });
});

describe(withServiceApproved, () => {
  it("adds and removes one name, leaving the rest of the list alone", () => {
    expect(withServiceApproved(["svc:api"], "svc:web", true)).toStrictEqual(["svc:api", "svc:web"]);
    expect(withServiceApproved(["svc:api", "svc:web"], "svc:web", false)).toStrictEqual([
      "svc:api",
    ]);
    // The list replaces what the machine had, so approving twice must not double the name.
    expect(withServiceApproved(["svc:web"], "svc:web", true)).toStrictEqual(["svc:web"]);
  });
});

describe(approvedServicesOf, () => {
  it("reads a machine's approvals back off the services", () => {
    const services = [
      service("svc:web", [host("1", { approved: true }), host("2")]),
      service("svc:api", [host("1")]),
      service("svc:db", [host("1", { approved: true })]),
    ];

    expect(approvedServicesOf(services, "1")).toStrictEqual(["svc:web", "svc:db"]);
    expect(approvedServicesOf(services, "2")).toStrictEqual([]);
  });
});

describe(machineServices, () => {
  it("joins what the machine announces with what it may host", () => {
    const machine = node(
      "1",
      [{ name: "svc:web", ports: ["tcp:443"], active: true }],
      ["svc:api", "svc:web"],
    );

    expect(machineServices(machine, [service("svc:web")])).toStrictEqual([
      { name: "svc:api", ports: [], announced: false, active: false, approved: true, known: false },
      {
        name: "svc:web",
        ports: ["tcp:443"],
        announced: true,
        active: true,
        approved: true,
        known: true,
      },
    ]);
  });
});

describe(hostedServices, () => {
  it("counts only what the machine advertises and may host", () => {
    const machine = node(
      "1",
      [
        { name: "svc:web", ports: [], active: true },
        { name: "svc:api", ports: [], active: false },
        { name: "svc:db", ports: [], active: true },
      ],
      ["svc:web", "svc:api"],
    );

    expect(hostedServices(machine)).toStrictEqual(["svc:web"]);
  });
});
