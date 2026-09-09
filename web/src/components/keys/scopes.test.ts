import { describe, expect, it } from "vitest";

import type { Me } from "~/auth/me.ts";
import { scopeItems, scopeItemsWithExisting, scopeLabel } from "~/components/keys/scopes.ts";

describe(scopeLabel, () => {
  it("names a known scope and echoes an unknown one", () => {
    expect(scopeLabel("dns:read")).toBe("DNS (read)");
    expect(scopeLabel("kitchen:sink")).toBe("kitchen:sink");
  });
});

describe(scopeItems, () => {
  it("offers only the scopes the caller holds", () => {
    const me: Me = {
      kind: "api_key",
      role: "network-admin",
      allAccess: false,
      scopes: ["dns", "policy_file:read"],
      permissions: { dns: true, "dns:read": true, "policy_file:read": true, users: false },
    };

    expect(scopeItems(me).map((item) => item.value)).toStrictEqual([
      "policy_file:read",
      "dns",
      "dns:read",
    ]);
  });
});

describe(scopeItemsWithExisting, () => {
  it("includes both caller scopes and pre-existing assigned scopes", () => {
    const me: Me = {
      kind: "api_key",
      role: "network-admin",
      allAccess: false,
      scopes: ["dns"],
      permissions: { dns: true, users: false },
    };

    const items = scopeItemsWithExisting(me, ["dns", "users"]);
    expect(items.map((item) => item.value)).toStrictEqual(["dns", "users"]);
  });
});
