import { describe, expect, it } from "vitest";

import type { AccessGraphEdge, AccessGraphNode } from "~/api/schema.gen.ts";
import {
  buildMatrix,
  clampCell,
  edgeListView,
  edgePreview,
  edgeRows,
  edgeSummary,
  fitsMatrix,
  groupEdges,
  maxMatrixNodes,
  nextCell,
  nodesById,
  ownerLabel,
  parsePort,
  portChips,
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

describe(portChips, () => {
  it("gathers the ports of a protocol behind it, in the order they arrived", () => {
    expect(portChips(["tcp:22", "tcp:443", "udp:53"])).toStrictEqual(["tcp:22, 443", "udp:53"]);
  });

  it("keeps ports without a protocol on their own", () => {
    expect(portChips(["22", "1-100"])).toStrictEqual(["22, 1-100"]);
  });

  it("says everything in words and drops what a wildcard makes moot", () => {
    expect(portChips(["*"])).toStrictEqual(["Every port"]);
    expect(portChips(["22", "*"])).toStrictEqual(["Every port"]);
    expect(portChips(["tcp:22", "tcp:*"])).toStrictEqual(["tcp:*"]);
  });

  it("names a protocol that has no ports", () => {
    expect(portChips(["icmp", "tcp:22"])).toStrictEqual(["icmp", "tcp:22"]);
  });

  it("drops a repeated entry", () => {
    expect(portChips(["tcp:22", "tcp:22"])).toStrictEqual(["tcp:22"]);
  });
});

describe(summarisePorts, () => {
  it("joins the chips, and says so when there are none", () => {
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

describe(fitsMatrix, () => {
  it("draws a tailnet up to the limit and nothing at all when empty", () => {
    expect(fitsMatrix(0)).toBe(false);
    expect(fitsMatrix(maxMatrixNodes)).toBe(true);
    expect(fitsMatrix(maxMatrixNodes + 1)).toBe(false);
  });
});

describe(buildMatrix, () => {
  const nodes = [node("2", "beta"), node("1", "alpha"), node("3", "gamma")];
  const matrix = buildMatrix(nodes, [edge("1", "2", { ports: ["tcp:22"] }), edge("3", "1")]);

  it("puts both axes in name order", () => {
    expect(matrix.columns.map((one) => one.name)).toStrictEqual(["alpha", "beta", "gamma"]);
    expect(matrix.rows.map((row) => row.src.name)).toStrictEqual(["alpha", "beta", "gamma"]);
  });

  it("fills a cell only where the policy opens the pair", () => {
    const [alpha] = matrix.rows;

    expect(alpha?.cells.map((cell) => cell.edge !== undefined)).toStrictEqual([false, true, false]);
    expect(alpha?.cells[0]?.self).toBe(true);
    expect(alpha?.cells[1]?.edge?.ports).toStrictEqual(["tcp:22"]);
  });

  it("counts the open pairs", () => {
    expect(matrix.open).toBe(2);
  });

  it("never fills the diagonal, even when the graph carries a self edge", () => {
    const self = buildMatrix([node("1", "alpha")], [edge("1", "1", { ports: ["*"] })]);

    expect(self.open).toBe(0);
    expect(self.rows[0]?.cells[0]?.edge).toBeUndefined();
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
