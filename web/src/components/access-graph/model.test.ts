import { describe, expect, it } from "vitest";

import type { AccessGraphEdge, AccessGraphNode } from "~/api/schema.gen.ts";
import {
  accessClasses,
  buildAccessMap,
  cellLabel,
  cellSummary,
  clampCell,
  edgeListView,
  edgePreview,
  edgeRows,
  edgeSummary,
  fitsMap,
  groupEdges,
  mapDensity,
  maxMapClasses,
  maxTileClasses,
  nextCell,
  openness,
  nodesById,
  ownerLabel,
  parsePort,
  portLines,
  sshLogins,
  sshPhrase,
  summarisePorts,
} from "~/components/access-graph/model.ts";

function node(id: string, name: string, extra: Partial<AccessGraphNode> = {}): AccessGraphNode {
  return { id, name, user: "ada", tags: [], online: true, routes: [], ...extra };
}

function edge(src: string, dst: string, extra: Partial<AccessGraphEdge> = {}): AccessGraphEdge {
  return {
    src,
    dst,
    ports: [],
    routes: [],
    sshUsers: [],
    sshCheck: false,
    capabilities: [],
    ...extra,
  };
}

describe(parsePort, () => {
  it("reads a rule that named no protocol", () => {
    expect(parsePort("22")).toStrictEqual({ protocol: null, ports: "22" });
    expect(parsePort("1-100")).toStrictEqual({ protocol: null, ports: "1-100" });
    expect(parsePort("*")).toStrictEqual({ protocol: null, ports: "*" });
  });

  it("reads a rule that named one", () => {
    expect(parsePort("tcp:22")).toStrictEqual({ protocol: "tcp", ports: "22" });
    expect(parsePort("udp:1-100")).toStrictEqual({ protocol: "udp", ports: "1-100" });
    expect(parsePort("tcp:*")).toStrictEqual({ protocol: "tcp", ports: "*" });
  });

  it("keeps a protocol that has no ports of its own", () => {
    expect(parsePort("icmp")).toStrictEqual({ protocol: "icmp", ports: "" });
    expect(parsePort("ipv6-icmp")).toStrictEqual({ protocol: "ipv6-icmp", ports: "" });
  });
});

describe(portLines, () => {
  it("gathers the ports of a protocol behind it, in the order they arrived", () => {
    expect(portLines(["tcp:22", "tcp:443", "udp:53"])).toStrictEqual(["tcp:22, 443", "udp:53"]);
  });

  it("keeps ports without a protocol on their own", () => {
    expect(portLines(["22", "1-100"])).toStrictEqual(["22, 1-100"]);
  });

  it("says everything in words and drops what a wildcard makes moot", () => {
    expect(portLines(["*"])).toStrictEqual(["Every port"]);
    expect(portLines(["22", "*"])).toStrictEqual(["Every port"]);
    expect(portLines(["tcp:22", "tcp:*"])).toStrictEqual(["tcp:*"]);
  });

  it("names a protocol that has no ports", () => {
    expect(portLines(["icmp", "tcp:22"])).toStrictEqual(["icmp", "tcp:22"]);
  });

  it("drops a repeated entry", () => {
    expect(portLines(["tcp:22", "tcp:22"])).toStrictEqual(["tcp:22"]);
  });
});

describe(summarisePorts, () => {
  it("joins the lines, and says so when there are none", () => {
    expect(summarisePorts(["tcp:22", "udp:53"])).toBe("tcp:22 · udp:53");
    expect(summarisePorts([])).toBe("No ports");
  });
});

describe(sshLogins, () => {
  it("names the logins, with a wildcard as any", () => {
    expect(sshLogins([])).toBe("");
    expect(sshLogins(["ada", "root"])).toBe("ada, root");
    expect(sshLogins(["*"])).toBe("Any login");
  });
});

describe(sshPhrase, () => {
  it("reads mid-sentence and carries the check the rule asks for", () => {
    expect(sshPhrase(edge("1", "2"))).toBe("");
    expect(sshPhrase(edge("1", "2", { sshUsers: ["*"] }))).toBe("SSH as any login");
    expect(sshPhrase(edge("1", "2", { sshUsers: ["ada"], sshCheck: true }))).toBe(
      "SSH as ada, after a check",
    );
  });
});

describe(edgeSummary, () => {
  it("lists everything the edge opens", () => {
    const open = edge("1", "2", {
      ports: ["tcp:22"],
      routes: ["10.0.0.0/24"],
      sshUsers: ["ada"],
      capabilities: ["tailscale.com/cap/ingress"],
    });

    expect(edgeSummary(open)).toBe("tcp:22 · Routes 10.0.0.0/24 · SSH as ada · 1 capability");
  });

  it("counts more than one capability and says when nothing is open", () => {
    expect(edgeSummary(edge("1", "2", { capabilities: ["one", "two"] }))).toBe("2 capabilities");
    expect(edgeSummary(edge("1", "2"))).toBe("Nothing open");
  });
});

describe(groupEdges, () => {
  const edges = [edge("1", "2"), edge("3", "1"), edge("2", "3"), edge("1", "1")];

  it("splits one machine's edges by direction and ignores a self edge", () => {
    const groups = groupEdges(edges, "1");

    expect(groups.reachable.map((one) => one.dst)).toStrictEqual(["2"]);
    expect(groups.reachedBy.map((one) => one.src)).toStrictEqual(["3"]);
  });

  it("gives a machine with no edges two empty lists", () => {
    expect(groupEdges(edges, "9")).toStrictEqual({ reachable: [], reachedBy: [] });
  });
});

describe(edgeRows, () => {
  const index = nodesById([node("2", "beta"), node("3", "alpha")]);

  it("names the machine at the other end and puts the rows in name order", () => {
    const rows = edgeRows([edge("1", "2"), edge("1", "3")], "dst", index);

    expect(rows.map((row) => row.node?.name)).toStrictEqual(["alpha", "beta"]);
  });

  it("keeps an edge whose machine the graph did not list, ordered by its id", () => {
    const rows = edgeRows([edge("1", "2"), edge("1", "9")], "dst", index);

    expect(rows.map((row) => row.nodeId)).toStrictEqual(["9", "2"]);
    expect(rows.map((row) => row.node?.name)).toStrictEqual([undefined, "beta"]);
  });

  it("reads the source end when that is the other machine", () => {
    const rows = edgeRows([edge("2", "1"), edge("3", "1")], "src", index);

    expect(rows.map((row) => row.node?.name)).toStrictEqual(["alpha", "beta"]);
  });
});

describe(nodesById, () => {
  it("looks a machine up by the id an edge carries", () => {
    const index = nodesById([node("1", "alpha"), node("2", "beta")]);

    expect(index.get("2")?.name).toBe("beta");
    expect(index.get("9")).toBeUndefined();
  });
});

describe(ownerLabel, () => {
  it("prefers tags, falls back to the owner and says when there is neither", () => {
    expect(ownerLabel(node("1", "alpha", { tags: ["tag:web"], user: "" }))).toBe("tag:web");
    expect(ownerLabel(node("1", "alpha"))).toBe("ada");
    expect(ownerLabel(node("1", "alpha", { user: "" }))).toBe("No owner");
  });

  it("names an edge whose machine the graph did not list", () => {
    expect(ownerLabel(nodesById([]).get("1"))).toBe("Unknown machine");
  });
});

describe(fitsMap, () => {
  it("draws a map up to the limit and nothing at all when empty", () => {
    expect(fitsMap(0)).toBe(false);
    expect(fitsMap(maxMapClasses)).toBe(true);
    expect(fitsMap(maxMapClasses + 1)).toBe(false);
  });
});

describe(openness, () => {
  it("grades an edge by its ports, with only a bare wildcard as everything", () => {
    expect(openness(edge("1", "2", { ports: ["*"] }))).toBe("all");
    expect(openness(edge("1", "2", { ports: ["tcp:22", "udp:*"] }))).toBe("some");
    expect(openness(edge("1", "2", { ports: ["tcp:22"] }))).toBe("some");
    expect(openness(edge("1", "2", { sshUsers: ["*"] }))).toBe("other");
  });
});

describe(cellLabel, () => {
  it("says in a few words which ports the cell opens", () => {
    expect(cellLabel(edge("1", "2", { ports: ["*"] }))).toBe("All ports");
    expect(cellLabel(edge("1", "2", { ports: ["*"], sshUsers: ["root"] }))).toBe("All ports · SSH");
    expect(cellLabel(edge("1", "2", { ports: ["tcp:22", "tcp:443"] }))).toBe("tcp:22, 443");
    expect(cellLabel(edge("1", "2", { ports: ["udp:*"] }))).toBe("udp:*");
  });

  it("names what is open when no port is", () => {
    expect(cellLabel(edge("1", "2", { sshUsers: ["*"] }))).toBe("SSH only");
    expect(cellLabel(edge("1", "2", { routes: ["10.0.0.0/8"] }))).toBe("Routes only");
    expect(cellLabel(edge("1", "2", { capabilities: ["x"] }))).toBe("Capabilities only");
    expect(
      cellLabel(edge("1", "2", { sshUsers: ["*"], routes: ["10.0.0.0/8"], capabilities: ["x"] })),
    ).toBe("SSH · Routes · Capabilities");
  });
});

describe(accessClasses, () => {
  // Three laptops that reach the two servers the same way, and each other on every port, are one
  // class; the servers, which reach nothing, are another; a server that also reaches the laptops
  // is a third.
  const laptops = ["1", "2", "3"].map((id) => node(id, `laptop-${id}`));
  const servers = ["8", "9"].map((id) => node(id, `server-${id}`, { user: "", tags: ["tag:srv"] }));
  const edges = [
    ...laptops.flatMap((from) =>
      laptops.filter((to) => to.id !== from.id).map((to) => edge(from.id, to.id, { ports: ["*"] })),
    ),
    ...laptops.flatMap((from) => servers.map((to) => edge(from.id, to.id, { ports: ["tcp:22"] }))),
  ];

  it("puts machines the policy treats alike in one class", () => {
    const classes = accessClasses([...laptops, ...servers], edges);
    const names = classes.map((members) => members.map((one) => one.name).toSorted());

    expect(names).toStrictEqual([
      ["laptop-1", "laptop-2", "laptop-3"],
      ["server-8", "server-9"],
    ]);
  });

  it("splits a class on a machine that reaches something the others do not", () => {
    const classes = accessClasses(
      [...laptops, ...servers],
      [
        ...edges,
        edge("9", "1", { ports: ["*"] }),
        edge("9", "2", { ports: ["*"] }),
        edge("9", "3", { ports: ["*"] }),
      ],
    );

    expect(
      classes.map((members) => members.length).toSorted((left, right) => left - right),
    ).toStrictEqual([1, 1, 3]);
  });

  it("keeps two pairs that reach only their own partner in two classes, not one", () => {
    const pairs = ["a", "b", "c", "d"].map((id) => node(id, id));
    const classes = accessClasses(pairs, [
      edge("a", "b", { ports: ["*"] }),
      edge("b", "a", { ports: ["*"] }),
      edge("c", "d", { ports: ["*"] }),
      edge("d", "c", { ports: ["*"] }),
    ]);

    expect(classes.map((members) => members.map((one) => one.id))).toStrictEqual([
      ["a", "b"],
      ["c", "d"],
    ]);
  });

  it("keeps the machines of a cycle apart", () => {
    const ring = ["a", "b", "c"].map((id) => node(id, id));
    const classes = accessClasses(ring, [
      edge("a", "c", { ports: ["*"] }),
      edge("c", "b", { ports: ["*"] }),
      edge("b", "a", { ports: ["*"] }),
    ]);

    expect(classes).toHaveLength(3);
  });

  it("tells capability sets apart, not just their presence", () => {
    const classes = accessClasses(
      [node("1", "a"), node("2", "b"), node("3", "c")],
      [edge("3", "1", { capabilities: ["x"] }), edge("3", "2", { capabilities: ["y"] })],
    );

    expect(classes).toHaveLength(3);
  });

  it("ignores a self edge and keeps a machine without edges", () => {
    const classes = accessClasses(
      [node("1", "a"), node("2", "b")],
      [edge("1", "1", { ports: ["*"] })],
    );

    expect(classes).toHaveLength(1);
    expect(classes[0]).toHaveLength(2);
  });
});

describe(buildAccessMap, () => {
  const nodes = [
    node("1", "laptop-1"),
    node("2", "laptop-2"),
    node("8", "web", { user: "", tags: ["tag:web"] }),
    node("9", "other", { user: "bob" }),
  ];
  const map = buildAccessMap(nodes, [
    edge("1", "2", { ports: ["*"] }),
    edge("2", "1", { ports: ["*"] }),
    edge("1", "8", { ports: ["tcp:443"] }),
    edge("2", "8", { ports: ["tcp:443"] }),
    edge("9", "8", { ports: ["tcp:443"] }),
  ]);

  it("names a class by what its machines share, a class of one by its machine", () => {
    expect(map.classes.map((group) => [group.label, group.detail, group.tagged])).toStrictEqual([
      ["ada", "laptop-1 +1", false],
      ["other", "bob", false],
      ["web", "tag:web", false],
    ]);
  });

  it("names a tagged class by its tags and puts owners before tags", () => {
    const tagged = buildAccessMap(
      [
        node("1", "web-1", { user: "", tags: ["tag:web"] }),
        node("2", "web-2", { user: "", tags: ["tag:web"] }),
        node("3", "laptop", { user: "zed@example.com" }),
      ],
      [edge("3", "1", { ports: ["tcp:443"] }), edge("3", "2", { ports: ["tcp:443"] })],
    );

    expect(tagged.classes.map((group) => [group.label, group.detail, group.tagged])).toStrictEqual([
      ["laptop", "zed", false],
      ["tag:web", "web-1 +1", true],
    ]);
  });

  it("fills a cell with the edge any pair between the two classes carries", () => {
    const [laptops] = map.rows;

    expect(laptops?.cells.map((cell) => cell.edge?.ports)).toStrictEqual([
      ["*"],
      undefined,
      ["tcp:443"],
    ]);
    expect(laptops?.cells[0]?.self).toBe(false);
  });

  it("marks a class of one against itself and counts machines and pairs", () => {
    const [, bob] = map.rows;

    expect(bob?.cells[1]?.self).toBe(true);
    expect(bob?.cells[1]?.edge).toBeUndefined();
    expect(map.machines).toBe(4);
    expect(map.open).toBe(5);
  });

  it("names a mixed class by its owners without their domains and counts the rest", () => {
    const mixed = buildAccessMap(
      ["a", "b", "c", "d"].map((user, index) =>
        node(String(index), user, { user: `${user}@example.com` }),
      ),
      [],
    );

    expect(mixed.classes.map((group) => group.label)).toStrictEqual(["a, b +2"]);
  });
});

describe(edgeListView, () => {
  const rows = edgeRows(
    Array.from({ length: 30 }, (_, index) => edge("1", String(index + 2))),
    "dst",
    nodesById([]),
  );

  it("stops a long list at the preview and counts what is left", () => {
    const view = edgeListView(rows, false);

    expect(view.rows).toHaveLength(edgePreview);
    expect(view.hidden).toBe(rows.length - edgePreview);
  });

  it("shows every edge once the list is opened", () => {
    expect(edgeListView(rows, true)).toStrictEqual({ rows, hidden: 0 });
  });

  it("holds nothing back from a list that already fits", () => {
    const short = rows.slice(0, edgePreview);

    expect(edgeListView(short, false)).toStrictEqual({ rows: short, hidden: 0 });
  });
});

describe(mapDensity, () => {
  it("keeps words in the cells while they fit across a screen", () => {
    expect(mapDensity(1)).toBe("tiles");
    expect(mapDensity(maxTileClasses)).toBe("tiles");
    expect(mapDensity(maxTileClasses + 1)).toBe("squares");
  });
});

describe(cellSummary, () => {
  const dst = { id: "0", members: [], label: "beta", detail: "", tagged: false };

  it("reads the pair out in full, or says why there is nothing to read", () => {
    expect(cellSummary({ dst, self: false, edge: edge("1", "2", { ports: ["tcp:22"] }) })).toBe(
      "tcp:22",
    );
    expect(cellSummary({ dst, self: false, edge: undefined })).toBe("Nothing open");
    expect(cellSummary({ dst, self: true, edge: undefined })).toBe("One machine against itself");
  });
});

describe(nextCell, () => {
  const middle = { row: 1, col: 1 };

  it("moves one cell at a time", () => {
    expect(nextCell("ArrowUp", middle, 3)).toStrictEqual({ row: 0, col: 1 });
    expect(nextCell("ArrowDown", middle, 3)).toStrictEqual({ row: 2, col: 1 });
    expect(nextCell("ArrowLeft", middle, 3)).toStrictEqual({ row: 1, col: 0 });
    expect(nextCell("ArrowRight", middle, 3)).toStrictEqual({ row: 1, col: 2 });
  });

  it("stops at the edges rather than wrapping", () => {
    expect(nextCell("ArrowUp", { row: 0, col: 0 }, 3)).toStrictEqual({ row: 0, col: 0 });
    expect(nextCell("ArrowLeft", { row: 0, col: 0 }, 3)).toStrictEqual({ row: 0, col: 0 });
    expect(nextCell("ArrowDown", { row: 2, col: 2 }, 3)).toStrictEqual({ row: 2, col: 2 });
    expect(nextCell("ArrowRight", { row: 2, col: 2 }, 3)).toStrictEqual({ row: 2, col: 2 });
  });

  it("goes to the ends of the row", () => {
    expect(nextCell("Home", middle, 3)).toStrictEqual({ row: 1, col: 0 });
    expect(nextCell("End", middle, 3)).toStrictEqual({ row: 1, col: 2 });
  });

  it("leaves every other key to the cell", () => {
    expect(nextCell("Enter", middle, 3)).toBeUndefined();
    expect(nextCell("a", middle, 3)).toBeUndefined();
  });
});

describe(clampCell, () => {
  it("holds the focus inside a matrix that lost machines", () => {
    expect(clampCell({ row: 5, col: 5 }, 3)).toStrictEqual({ row: 2, col: 2 });
    expect(clampCell({ row: 1, col: 2 }, 3)).toStrictEqual({ row: 1, col: 2 });
    expect(clampCell({ row: 0, col: 0 }, 0)).toStrictEqual({ row: 0, col: 0 });
  });
});
