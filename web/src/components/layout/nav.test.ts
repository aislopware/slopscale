import { SquaresFourIcon } from "@phosphor-icons/react";
import { describe, expect, it } from "vitest";

import type { Me } from "~/auth/me.ts";
import { isActive, pagesOf, placeOf, visibleGroups } from "~/components/layout/nav.ts";

function member(permissions: Me["permissions"]): Me {
  return {
    kind: "session",
    role: "member",
    allAccess: false,
    scoped: false,
    scopes: [],
    permissions,
  };
}

describe(visibleGroups, () => {
  it("keeps only the children a caller may read and drops a branch left with none", () => {
    const groups = visibleGroups(member({ "policy_file:read": true }));
    const labels = groups.flatMap((group) => group.items.map((item) => item.label));

    expect(labels).toContain("Access controls");
    expect(labels).toContain("Keys");
    expect(labels).not.toContain("Integrations");

    const keys = groups.flatMap((group) => group.items).find((item) => item.label === "Keys");

    expect(keys?.children?.map((child) => child.label)).toStrictEqual(["API keys"]);
  });
});

const reader = member({ "devices:core:read": true, "policy_file:read": true });

describe("a signed-in member", () => {
  const user = {
    id: "7",
    name: "ada",
    createdAt: "",
    displayName: "",
    email: "",
    providerId: "",
    provider: "",
    profilePicUrl: "",
    role: "member",
    approved: true,
    approvedAt: null,
  };

  it("gets their machines, their keys, their access and their sessions", () => {
    const groups = visibleGroups({ ...member({}), user });
    const labels = groups.flatMap((group) => group.items.map((item) => item.label));

    expect(labels).toStrictEqual(["Overview", "Machines", "Keys", "My access", "Settings"]);

    const settings = groups
      .flatMap((group) => group.items)
      .find((item) => item.label === "Settings");

    expect(settings?.children?.map((child) => child.label)).toStrictEqual(["Sessions"]);
  });

  it("gets none of it through a key minted with scopes", () => {
    const groups = visibleGroups({ ...member({}), user, scoped: true });
    const labels = groups.flatMap((group) => group.items.map((item) => item.label));

    expect(labels).toStrictEqual(["Overview", "Keys", "My access"]);
  });
});

describe(placeOf, () => {
  const groups = visibleGroups(reader);

  it("names the branch and the page under it", () => {
    const place = placeOf(groups, "/policy/postures");

    expect(place?.item.label).toBe("Access controls");
    expect(place?.child?.label).toBe("Postures");
  });

  it("names a page without a branch on its own", () => {
    const place = placeOf(groups, "/machines/42");

    expect(place?.item.label).toBe("Machines");
    expect(place?.child).toBeUndefined();
    expect(placeOf(groups, "/nowhere")).toBeUndefined();
  });
});

describe(isActive, () => {
  const overview = { to: "/", label: "Overview", icon: SquaresFourIcon, exact: true } as const;
  const keys = { to: "/keys", label: "Keys" } as const;

  it("matches a path and what is under it, and the overview only exactly", () => {
    expect(isActive(keys, "/keys/api")).toBe(true);
    expect(isActive(keys, "/keysmith")).toBe(false);
    expect(isActive(overview, "/keys")).toBe(false);
    expect(isActive(overview, "/")).toBe(true);
  });
});

describe(pagesOf, () => {
  it("lists a branch's pages with the branch as their hint", () => {
    const pages = pagesOf(visibleGroups(reader));
    const rules = pages.find((page) => page.to === "/policy/rules");

    expect(rules).toMatchObject({ label: "Rules", hint: "Access controls" });
    expect(pages.some((page) => page.to === "/policy")).toBe(false);
  });
});
