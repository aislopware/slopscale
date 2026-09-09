/**
 * The shape of a policy file as `hscontrol/policy/v2` reads it: every section, every key inside a
 * rule and what each one holds. The linter checks a draft against it, the completions offer it and
 * the hover explains it, so the three agree.
 */

/** What a value is expected to be, for the type check and the completion. */
export type Shape = "object" | "array" | "string" | "bool" | "aliases" | "strings" | "postureRefs";

export interface KeyInfo {
  readonly name: string;
  readonly doc: string;
  readonly shape: Shape;
  /** The keys the value may hold, when it is an object with a fixed set of them. */
  readonly keys?: readonly KeyInfo[];
}

const srcPosture: KeyInfo = {
  name: "srcPosture",
  doc: "Postures a source must satisfy one of. Without it, defaultSrcPosture applies, and an empty list waives it.",
  shape: "postureRefs",
};

export const aclKeys: readonly KeyInfo[] = [
  { name: "action", doc: 'Always "accept"; an ACL only ever allows.', shape: "string" },
  {
    name: "proto",
    doc: "One protocol the rule is limited to, such as tcp or udp, or a number from 0 to 255. Leave it out to cover every protocol.",
    shape: "string",
  },
  {
    name: "src",
    doc: "Who may connect: users, groups, tags, autogroups, hosts, addresses, ranges or *.",
    shape: "aliases",
  },
  {
    name: "dst",
    doc: 'What they may reach, each with its ports: "tag:web:443", "group:eng:*" or "*:*".',
    shape: "aliases",
  },
  srcPosture,
];

export const grantKeys: readonly KeyInfo[] = [
  {
    name: "src",
    doc: "Who may connect: users, groups, tags, autogroups, hosts, addresses, ranges or *.",
    shape: "aliases",
  },
  {
    name: "dst",
    doc: "What they may reach, without ports; the ip list carries those.",
    shape: "aliases",
  },
  {
    name: "ip",
    doc: 'The network access granted: "*", ports such as "443" or "8000-8080", or "tcp:22".',
    shape: "strings",
  },
  {
    name: "app",
    doc: "Application capabilities granted, keyed by a domain-qualified name such as example.com/cap, each with a list of objects.",
    shape: "object",
  },
  {
    name: "via",
    doc: "Tags of the routers the traffic must go through.",
    shape: "aliases",
  },
  srcPosture,
];

export const sshKeys: readonly KeyInfo[] = [
  {
    name: "action",
    doc: '"accept" lets the session through; "check" asks the user to re-authenticate first.',
    shape: "string",
  },
  {
    name: "src",
    doc: "Who may open a session: users, groups, tags or autogroups.",
    shape: "aliases",
  },
  {
    name: "dst",
    doc: "Which machines: users (their own machines), tags or autogroups.",
    shape: "aliases",
  },
  {
    name: "users",
    doc: 'The login names allowed on the machine, such as "root", "ubuntu" or autogroup:nonroot.',
    shape: "strings",
  },
  {
    name: "checkPeriod",
    doc: 'How long a re-authentication holds with action "check", such as "12h" or "always".',
    shape: "string",
  },
  {
    name: "acceptEnv",
    doc: 'Environment variable names the client may pass, with "*" as a wildcard.',
    shape: "strings",
  },
  {
    name: "recorder",
    doc: "Tags, hosts or addresses of the session recorders; empty means the tailnet default.",
    shape: "aliases",
  },
  {
    name: "enforceRecorder",
    doc: "Refuse the session when no recorder is reachable rather than letting it go unrecorded.",
    shape: "bool",
  },
];

export const nodeAttrKeys: readonly KeyInfo[] = [
  { name: "target", doc: "The machines the attributes apply to.", shape: "aliases" },
  { name: "attr", doc: 'Attribute names, such as "magicdns-aaaa".', shape: "strings" },
  {
    name: "app",
    doc: "Application capabilities to stamp on the targets, keyed by a domain-qualified name.",
    shape: "object",
  },
  {
    name: "ipPool",
    doc: "Ranges the targets get their tailnet addresses from, inside the CGNAT range.",
    shape: "strings",
  },
];

export const autoApproverKeys: readonly KeyInfo[] = [
  {
    name: "routes",
    doc: "Each range to the users, groups or tags whose routes inside it are approved automatically.",
    shape: "object",
  },
  {
    name: "exitNode",
    doc: "The users, groups or tags whose machines may become exit nodes without approval.",
    shape: "aliases",
  },
  {
    name: "services",
    doc: "Each service, svc:web, to the users, groups or tags whose machines may host it without approval.",
    shape: "object",
  },
];

export const testKeys: readonly KeyInfo[] = [
  { name: "src", doc: "One source the test connects from.", shape: "string" },
  {
    name: "proto",
    doc: "tcp, udp or sctp; empty covers the client's default set.",
    shape: "string",
  },
  {
    name: "accept",
    doc: 'Destinations with one port each that must be reachable: "tag:web:443".',
    shape: "strings",
  },
  { name: "deny", doc: "Destinations with one port each that must be blocked.", shape: "strings" },
];

export const sshTestKeys: readonly KeyInfo[] = [
  { name: "src", doc: "One source the test opens sessions from.", shape: "string" },
  { name: "dst", doc: "Tags, users or autogroups the test opens sessions to.", shape: "aliases" },
  { name: "accept", doc: "Login names that must get through on every dst.", shape: "strings" },
  { name: "deny", doc: "Login names that must be refused on every dst.", shape: "strings" },
  {
    name: "check",
    doc: 'Login names that must get through with action "check".',
    shape: "strings",
  },
];

export const sections: readonly KeyInfo[] = [
  {
    name: "groups",
    doc: 'Named sets of users, such as "group:eng", each member written with an @ in it.',
    shape: "object",
  },
  {
    name: "hosts",
    doc: "Names for addresses and ranges, to use in place of them.",
    shape: "object",
  },
  {
    name: "tagOwners",
    doc: "Which users, groups or tags may put each tag on a machine. A tag must be here before a rule can name it.",
    shape: "object",
  },
  {
    name: "acls",
    doc: "Rules in the older form: action, src and dst with ports.",
    shape: "array",
    keys: aclKeys,
  },
  {
    name: "grants",
    doc: "Who may reach what, on which ports or with which application capabilities.",
    shape: "array",
    keys: grantKeys,
  },
  {
    name: "nodeAttrs",
    doc: "Attributes and application capabilities stamped on machines.",
    shape: "array",
    keys: nodeAttrKeys,
  },
  {
    name: "autoApprovers",
    doc: "Routes, exit nodes and service hosts approved without an administrator.",
    shape: "object",
    keys: autoApproverKeys,
  },
  {
    name: "ssh",
    doc: "Who may SSH where, as which login, when the client runs Tailscale SSH.",
    shape: "array",
    keys: sshKeys,
  },
  {
    name: "tests",
    doc: "Assertions the policy must satisfy before it is accepted.",
    shape: "array",
    keys: testKeys,
  },
  { name: "sshTests", doc: "Assertions about the SSH rules.", shape: "array", keys: sshTestKeys },
  {
    name: "randomizeClientPort",
    doc: "Tell clients to pick a random WireGuard port rather than 41641.",
    shape: "bool",
  },
  {
    name: "postures",
    doc: 'Each "posture:name" to the expressions a machine must satisfy, such as "node:os == \'macos\'".',
    shape: "object",
  },
  {
    name: "defaultSrcPosture",
    doc: "Postures applied to every rule that has no srcPosture of its own.",
    shape: "postureRefs",
  },
];

export function sectionInfo(name: string): KeyInfo | undefined {
  return sections.find((section) => section.name === name);
}

export const autogroupDocs: Readonly<Record<string, string>> = {
  "autogroup:internet": "Everything outside the tailnet, through an exit node. Destination only.",
  "autogroup:member": "Every machine a user owns.",
  "autogroup:tagged": "Every tagged machine.",
  "autogroup:self": "The machines of the same user as the source. Destination only.",
  "autogroup:shared": "The machines of users the destination is shared with. Source only.",
  "autogroup:danger-all": "Every machine, tagged or not. Source only.",
  "autogroup:nonroot": "Any login except root, in an SSH users list.",
  "autogroup:owner": "The machines of the owner.",
  "autogroup:admin": "The machines of every admin.",
  "autogroup:network-admin": "The machines of every network admin.",
  "autogroup:it-admin": "The machines of every IT admin.",
  "autogroup:auditor": "The machines of every auditor.",
};
