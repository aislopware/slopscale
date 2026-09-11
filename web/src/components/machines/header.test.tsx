import { describe, expect, it } from "vitest";

import type { Me } from "~/auth/me.ts";

import { sshDisabledReason } from "./header.tsx";

const mockMeWithUser: Me = {
  allAccess: true,
  scoped: false,
  kind: "api_key",
  permissions: {
    all: true,
    "all:read": true,
    auth_keys: true,
    "auth_keys:read": true,
    "devices:core": true,
    "devices:core:read": true,
    "devices:posture_attributes": true,
    "devices:posture_attributes:read": true,
    "devices:routes": true,
    "devices:routes:read": true,
    dns: true,
    "dns:read": true,
    feature_settings: true,
    "feature_settings:read": true,
    "logs:configuration": true,
    "logs:configuration:read": true,
    oauth_keys: true,
    "oauth_keys:read": true,
    policy_file: true,
    "policy_file:read": true,
    services: true,
    "services:read": true,
    users: true,
    "users:read": true,
    webhooks: true,
    "webhooks:read": true,
  },
  role: "admin",
  scopes: [],
  user: {
    approved: true,
    approvedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    displayName: "Admin",
    email: "admin@example.com",
    id: "1",
    name: "admin",
    profilePicUrl: "",
    provider: "",
    providerId: "",
    role: "admin",
  },
};

const mockMeWithoutUser: Me = {
  allAccess: true,
  scoped: false,
  kind: "api_key",
  permissions: mockMeWithUser.permissions,
  role: "admin",
  scopes: [],
};

describe(sshDisabledReason, () => {
  it("refuses a caller without a user, such as an API key", () => {
    expect(sshDisabledReason({ online: true, sshServer: true }, mockMeWithoutUser)).toBe(
      "SSH requires a user login",
    );
  });

  it("refuses a machine that is not connected", () => {
    expect(sshDisabledReason({ online: false, sshServer: true }, mockMeWithUser)).toBe(
      "Machine is offline",
    );
  });

  it("refuses a machine that does not run Tailscale SSH", () => {
    expect(sshDisabledReason({ online: true, sshServer: false }, mockMeWithUser)).toBe(
      "Machine does not run Tailscale SSH",
    );
  });

  it("allows a connected machine for a signed-in operator", () => {
    expect(sshDisabledReason({ online: true, sshServer: true }, mockMeWithUser)).toBeUndefined();
  });
});
