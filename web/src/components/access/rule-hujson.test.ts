import { describe, expect, it } from "vitest";

import type { Group, Posture } from "~/api/queries.ts";
import type { RuleDraft } from "~/components/access/rule-draft.ts";
import {
  changedConditions,
  parseRuleSnippet,
  problemLine,
  ruleSnippet,
} from "~/components/access/rule-hujson.ts";

const stamp = "2026-01-01T12:00:00Z";

function group(id: string, name: string, builtin = ""): Group {
  return {
    builtin,
    source: "",
    createdAt: stamp,
    description: "",
    expiries: [],
    id,
    name,
    nodeIds: [],
    requestable: false,
    updatedAt: stamp,
    userIds: [],
  };
}

function posture(id: string, name: string): Posture {
  return { createdAt: stamp, description: "", expressions: [], id, name, updatedAt: stamp };
}

const groups = [
  group("1", "All machines", "all"),
  group("2", "Own machines", "self"),
  group("3", "Engineering"),
  group("4", "Production servers"),
];
const postures = [posture("7", "Managed laptops")];

function draft(patch: Partial<RuleDraft>): RuleDraft {
  return {
    name: "Engineers",
    description: "",
    sources: [],
    destinations: [],
    postures: [],
    protocol: "all",
    ports: "",
    bidirectional: false,
    enabled: true,
    expiring: false,
    expires: "",
    ...patch,
  };
}

function parsed(text: string): ReturnType<typeof parseRuleSnippet> {
  return parseRuleSnippet(text, groups, postures);
}

/** What reading the draft's own snippet back would change in it; "refused" if it did not parse. */
function roundTripChanges(before: RuleDraft): Partial<RuleDraft> | "refused" {
  const result = parsed(ruleSnippet(before, groups, postures));

  return result.ok ? changedConditions(before, result.patch) : "refused";
}

/** The first problem as the line under the editor writes it, or "parsed". */
function firstLine(text: string): string {
  const result = parsed(text);
  const problem = result.ok ? undefined : result.problems[0];

  return problem === undefined ? "parsed" : problemLine(text, problem);
}

/** The text the first problem underlines, or "parsed". */
function underlined(text: string): string {
  const result = parsed(text);
  const problem = result.ok ? undefined : result.problems[0];

  return problem === undefined ? "parsed" : text.slice(problem.from, problem.to);
}

describe(ruleSnippet, () => {
  it("writes the rule as the grant the server compiles, groups and postures by name", () => {
    const text = ruleSnippet(
      draft({
        sources: ["3"],
        destinations: ["4", "2"],
        postures: ["7"],
        protocol: "tcp",
        ports: "22, 443",
      }),
      groups,
      postures,
    );

    expect(text).toBe(
      [
        "{",
        '  "src": ["accessgroup:Engineering"],',
        '  "dst": ["accessgroup:Production servers", "autogroup:self"],',
        '  "ip": ["tcp:22,443"],',
        '  "srcPosture": ["posture:#Managed laptops"],',
        "}",
      ].join("\n"),
    );
  });

  it("writes every protocol as a wildcard and ICMP for both IP versions", () => {
    expect(ruleSnippet(draft({ protocol: "all" }), groups, postures)).toContain('"ip": ["*"]');
    expect(ruleSnippet(draft({ protocol: "udp" }), groups, postures)).toContain('"ip": ["udp:*"]');
    expect(ruleSnippet(draft({ protocol: "icmp" }), groups, postures)).toContain(
      '"ip": ["icmp:*", "ipv6-icmp:*"]',
    );
  });
});

describe(parseRuleSnippet, () => {
  it.each([
    draft({
      sources: ["3"],
      destinations: ["4", "2"],
      postures: ["7"],
      protocol: "tcp",
      ports: "22, 443",
    }),
    draft({ sources: ["1"], destinations: ["1"], protocol: "all" }),
    draft({ sources: ["3"], destinations: ["4"], protocol: "udp", ports: "" }),
    draft({ sources: ["3"], destinations: ["4"], protocol: "icmp" }),
    draft({ sources: [], destinations: [] }),
  ])("reads back what it wrote, leaving the draft unchanged (%#)", (before) => {
    expect(roundTripChanges(before)).toStrictEqual({});
  });

  it("takes an edited snippet with comments and trailing commas", () => {
    const result = parsed(`{
      // engineers reach production on SSH and HTTPS
      "src": ["accessgroup:Engineering",],
      "dst": ["accessgroup:4"],
      "ip": ["tcp:22", "tcp:443"],
    }`);

    expect(result).toStrictEqual({
      ok: true,
      patch: {
        sources: ["3"],
        destinations: ["4"],
        postures: [],
        protocol: "tcp",
        ports: "22, 443",
      },
    });
  });

  it("reports a syntax error where it is, as the policy file editor does", () => {
    const text = '{\n  "src": ["accessgroup:Engineering"\n  "dst": [],\n  "ip": ["*"],\n}';

    expect(firstLine(text)).toMatch(/^Line [23]: \S/v);
  });

  it.each([
    ['["*"]', "A rule is one grant"],
    ['{ "src": ["accessgroup:Nobody"], "dst": [], "ip": ["*"] }', 'No group is named "Nobody"'],
    ['{ "src": ["group:eng"], "dst": [], "ip": ["*"] }', 'Name a group as "accessgroup:<name>"'],
    ['{ "src": ["autogroup:self"], "dst": [], "ip": ["*"] }', 'only works in "dst"'],
    ['{ "src": [], "dst": [] }', 'Add "ip"'],
    ['{ "src": [], "dst": [], "ip": [] }', '"ip" lists what the rule allows'],
    ['{ "src": [], "dst": [], "ip": ["tcp:22", "udp:53"] }', "A rule allows one protocol"],
    ['{ "src": [], "dst": [], "ip": ["443"] }', "A rule allows one protocol"],
    ['{ "src": [], "dst": [], "ip": ["tcp:99999"] }', "Ports go up to 65535"],
    ['{ "src": [], "dst": [], "ip": ["*"], "via": ["tag:exit"] }', '"via" goes in the policy file'],
    ['{ "src": [], "src": [], "dst": [], "ip": ["*"] }', '"src" appears twice'],
    ['{ "src": "accessgroup:Engineering", "dst": [], "ip": ["*"] }', '"src" is a list'],
    [
      '{ "src": [], "dst": [], "ip": ["*"], "srcPosture": ["posture:#Gone"] }',
      'No posture is named "Gone"',
    ],
  ])("refuses %s", (text, message) => {
    expect(firstLine(text)).toContain(message);
  });

  it("underlines the entry that is wrong", () => {
    expect(underlined('{ "src": ["accessgroup:Nobody"], "dst": [], "ip": ["*"] }')).toBe(
      '"accessgroup:Nobody"',
    );
  });
});
