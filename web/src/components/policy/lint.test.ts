import { describe, expect, it } from "vitest";

import { lintPolicy } from "~/components/policy/lint.ts";

function messages(text: string): string[] {
  return lintPolicy(text).problems.map((problem) => problem.message);
}

describe(lintPolicy, () => {
  it("grants the tailscale.com capabilities the server accepts and refuses the rest", () => {
    expect(
      messages(`{
        "tagOwners": { "tag:gw": ["alice@"] },
        "grants": [
          { "src": ["autogroup:member"], "dst": ["tag:gw"], "app": { "tailscale.com/cap/relay": [{}] } },
          { "src": ["autogroup:member"], "dst": ["tag:gw"], "app": { "tailscale.com/cap/ingress": [{}] } },
        ],
      }`),
    ).toStrictEqual([
      "Capabilities under tailscale.com are reserved, except tailscale.com/cap/drive, tailscale.com/cap/relay, tailscale.com/cap/webui, tailscale.com/cap/kubernetes, tailscale.com/cap/tsidp, tailscale.com/cap/secrets",
    ]);
  });

  it("takes a service as a destination and as an auto approver key", () => {
    expect(
      messages(`{
        "tagOwners": { "tag:web": ["alice@"] },
        "autoApprovers": { "services": { "svc:web": ["tag:web"] } },
        "grants": [{ "src": ["autogroup:member"], "dst": ["svc:web"], "ip": ["tcp:443"] }],
      }`),
    ).toStrictEqual([]);
  });

  it("refuses a service as a source and a name that is not a label", () => {
    expect(
      messages(`{
        "autoApprovers": { "services": { "svc:Web": ["alice@"] } },
        "grants": [{ "src": ["svc:web"], "dst": ["autogroup:member"], "ip": ["*"] }],
      }`),
    ).toStrictEqual([
      '"svc:Web" is not a service name such as "svc:web"',
      "A service can only be a destination",
    ]);
  });
});
