import { describe, expect, it } from "vitest";

import type { Invite } from "~/api/queries.ts";
import {
  defaultInviteExpiry,
  invitableRoles,
  inviteExpiryLabel,
  inviteExpiryOptions,
  inviteState,
  openInvites,
  toInviteExpiry,
  sentence,
} from "~/components/users/invites.ts";

const stamp = "2026-01-01T00:00:00Z";

function invite(id: string, overrides: Partial<Invite> = {}): Invite {
  return {
    accepted: false,
    acceptedAt: null,
    createdAt: stamp,
    createdBy: "1",
    email: `person-${id}@example.com`,
    expired: false,
    expiresAt: stamp,
    groupIds: [],
    id,
    role: "member",
    ...overrides,
  };
}

describe("the invite expiry", () => {
  it("offers a span in days against the duration the API takes", () => {
    expect(inviteExpiryOptions).toStrictEqual([
      { value: "24h", label: "1 day" },
      { value: "168h", label: "7 days" },
      { value: "720h", label: "30 days" },
    ]);
  });

  it("defaults to the week the server itself would apply", () => {
    expect(defaultInviteExpiry).toBe("168h");
    expect(inviteExpiryLabel(defaultInviteExpiry)).toBe("7 days");
  });

  it("falls back to the default for anything a select could not have offered", () => {
    expect(toInviteExpiry("720h")).toBe("720h");
    expect(toInviteExpiry("5000h")).toBe("168h");
    expect(toInviteExpiry(null)).toBe("168h");
  });

  it("shows an unknown duration as it came, rather than inventing a span", () => {
    expect(inviteExpiryLabel("36h")).toBe("36h");
  });
});

describe("what an invitation may carry", () => {
  it("never offers owner, which is transferred rather than handed out", () => {
    expect(invitableRoles.map((role) => role.value)).not.toContain("owner");
    expect(invitableRoles.map((role) => role.value)).toContain("admin");
    expect(invitableRoles.map((role) => role.value)).toContain("member");
  });
});

describe(openInvites, () => {
  it("leaves out the ones that became a user row", () => {
    const rows = openInvites([
      invite("1"),
      invite("2", { accepted: true, acceptedAt: stamp, acceptedUserId: "9" }),
      invite("3", { expired: true }),
    ]);

    expect(rows.map((row) => row.id)).toStrictEqual(["1", "3"]);
  });

  it("tells an expired invitation from one still waiting", () => {
    expect(inviteState(invite("1"))).toBe("pending");
    expect(inviteState(invite("2", { expired: true }))).toBe("expired");
    expect(inviteState(invite("3", { accepted: true, expired: true }))).toBe("accepted");
  });
});

describe(sentence, () => {
  it("turns a server error into a sentence", () => {
    expect(sentence("no mail server is configured")).toBe("No mail server is configured.");
    expect(sentence("Refused by the relay.")).toBe("Refused by the relay.");
    expect(sentence("  ")).toBe("");
  });
});
