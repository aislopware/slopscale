import { describe, expect, it } from "vitest";

import {
  appDomainError,
  appNameError,
  appRouteError,
  appValidationError,
  appsForNode,
  connectorLabel,
  connectorMachineCounts,
  connectorTagError,
  connectorsError,
  countApps,
  domainCovers,
  domainsError,
  everyConnector,
  isAppDomain,
  learnedForApp,
  learnedRoutes,
  machinesLabel,
  normalizeConnectorTag,
  normalizeDomain,
  pendingSearch,
  routesError,
  totalPendingRoutes,
} from "~/components/apps/model.ts";
import type { AppDraft } from "~/components/apps/model.ts";

describe(connectorLabel, () => {
  it("formats wildcard as Every connector", () => {
    expect(connectorLabel("*")).toBe("Every connector");
  });

  it("keeps tag selectors intact", () => {
    expect(connectorLabel("tag:app")).toBe("tag:app");
  });
});

describe("sums and counts", () => {
  const nodes = [
    { online: true, learnedRoutes: 5, pending: 2 },
    { online: false, learnedRoutes: 3, pending: 0 },
    { online: true, learnedRoutes: 2, pending: 1 },
  ];

  it("calculates total pending routes", () => {
    expect(totalPendingRoutes(nodes)).toBe(3);
    expect(totalPendingRoutes([])).toBe(0);
  });

  it("calculates online and total machine counts", () => {
    expect(connectorMachineCounts(nodes)).toStrictEqual({ online: 2, total: 3 });
    expect(connectorMachineCounts([])).toStrictEqual({ online: 0, total: 0 });
  });

  it("formats app count", () => {
    expect(countApps(0)).toBe("0 apps");
    expect(countApps(1)).toBe("1 app");
    expect(countApps(5)).toBe("5 apps");
  });
});

describe("domain validation", () => {
  it("normalizes domains", () => {
    expect(normalizeDomain(" EXAMPLE.COM. ")).toBe("example.com");
  });

  it("accepts valid domains and wildcard domains", () => {
    expect(isAppDomain("example.com")).toBe(true);
    expect(isAppDomain("*.example.com")).toBe(true);
    expect(isAppDomain("sub.domain.corp")).toBe(true);
    expect(isAppDomain("*.sub.domain.corp")).toBe(true);
  });

  it("rejects invalid domains", () => {
    expect(isAppDomain("")).toBe(false);
    expect(isAppDomain("*")).toBe(false);
    expect(isAppDomain("*.")).toBe(false);
    expect(isAppDomain("*.*.example.com")).toBe(false);
    expect(isAppDomain("example.*.com")).toBe(false);
  });

  it("reports appDomainError message", () => {
    expect(appDomainError("")).toBe("Enter a domain.");
    expect(appDomainError("example.com")).toBeNull();
    expect(appDomainError("invalid")).toContain("Enter a domain such as");
  });

  it("validates list of domains", () => {
    expect(domainsError(["example.com", "*.internal.net"])).toBeNull();
    expect(domainsError(["example.com", "bad domain"])).toContain("Enter a domain such as");
  });
});

describe("route validation", () => {
  it("accepts valid CIDRs", () => {
    expect(appRouteError("10.0.0.0/24")).toBeNull();
    expect(appRouteError("192.168.1.1/32")).toBeNull();
    expect(appRouteError("2001:db8::/32")).toBeNull();
  });

  it("rejects invalid route strings", () => {
    expect(appRouteError("")).toBe("Enter a route CIDR.");
    expect(appRouteError("10.0.0.1")).toContain("not a CIDR");
    expect(appRouteError("not-an-ip/24")).toContain("not a valid IP");
    expect(appRouteError("10.0.0.0/33")).toContain("invalid prefix length");
  });

  it("rejects default routes", () => {
    expect(appRouteError("0.0.0.0/0")).toContain("Default routes are not allowed");
    expect(appRouteError("::/0")).toContain("Default routes are not allowed");
  });

  it("validates list of routes", () => {
    expect(routesError(["10.0.0.0/24", "172.16.0.0/16"])).toBeNull();
    expect(routesError(["10.0.0.0/24", "0.0.0.0/0"])).toContain("Default routes");
  });
});

describe("connector tag validation", () => {
  it("normalizes connector tags", () => {
    expect(normalizeConnectorTag("server")).toBe("tag:server");
    expect(normalizeConnectorTag("tag:app")).toBe("tag:app");
    expect(normalizeConnectorTag("*")).toBe("*");
    expect(normalizeConnectorTag("")).toBe("*");
  });

  it("accepts valid tags and wildcards", () => {
    expect(connectorTagError("*")).toBeNull();
    expect(connectorTagError("tag:server")).toBeNull();
    expect(connectorTagError("server")).toBeNull();
  });

  it("rejects invalid connector tags", () => {
    expect(connectorTagError("tag:")).toContain("Enter a valid tag name");
    expect(connectorTagError("tag:has spaces")).toContain("not a valid tag");
  });

  it("validates list of connectors", () => {
    expect(connectorsError(["tag:app", "*"])).toBeNull();
    expect(connectorsError(["tag:invalid tag!"])).toContain("not a valid tag");
  });
});

describe(appValidationError, () => {
  const validDraft: AppDraft = {
    name: "my-app",
    description: "An internal app",
    domains: ["example.com"],
    connectors: ["tag:app"],
    routes: ["10.0.0.0/24"],
  };

  it("accepts a valid draft", () => {
    expect(appValidationError(validDraft)).toBeNull();
  });

  it("validates name", () => {
    expect(appNameError("")).toBe("Enter a name.");
    expect(appNameError("a".repeat(64))).toContain("63 characters or fewer");
    expect(appValidationError({ ...validDraft, name: "" })).toBe("Enter a name.");
  });

  it("requires at least one domain or route", () => {
    expect(appValidationError({ ...validDraft, domains: [], routes: [] })).toBe(
      "An app needs at least one domain or route.",
    );
  });

  it("validates domains in draft", () => {
    expect(appValidationError({ ...validDraft, domains: ["bad domain"] })).toContain(
      "Enter a domain such as",
    );
  });

  it("validates routes in draft", () => {
    expect(appValidationError({ ...validDraft, routes: ["0.0.0.0/0"] })).toContain(
      "Default routes",
    );
  });
});

describe("connector selectors", () => {
  it("treats an empty list and the wildcard as every connector", () => {
    expect(everyConnector([])).toBe(true);
    expect(everyConnector(["*"])).toBe(true);
    expect(everyConnector(["tag:app"])).toBe(false);
  });
});

describe(machinesLabel, () => {
  it("reads online out of total", () => {
    expect(machinesLabel({ online: 2, total: 3 })).toBe("2/3");
    expect(machinesLabel({ online: 0, total: 0 })).toBe("0/0");
  });
});

describe(pendingSearch, () => {
  it("names the one connector with routes waiting", () => {
    expect(
      pendingSearch([
        { name: "relay-1", pending: 2 },
        { name: "relay-2", pending: 0 },
      ]),
    ).toStrictEqual({ pending: true, q: "relay-1" });
  });

  it("names nobody when none or several are waiting", () => {
    expect(pendingSearch([{ name: "relay-1", pending: 0 }])).toStrictEqual({ pending: true });
    expect(
      pendingSearch([
        { name: "relay-1", pending: 1 },
        { name: "relay-2", pending: 3 },
      ]),
    ).toStrictEqual({ pending: true });
  });

  it("keeps the pending chip on whatever the connectors are", () => {
    expect(pendingSearch([]).pending).toBe(true);
  });
});

describe(appsForNode, () => {
  const apps = [
    { name: "crm", nodes: [{ nodeId: "1" }, { nodeId: "2" }] },
    { name: "wiki", nodes: [{ nodeId: "2" }] },
    { name: "billing", nodes: [] },
  ];

  it("keeps the apps the machine connects", () => {
    expect(appsForNode(apps, "2").map((app) => app.name)).toStrictEqual(["crm", "wiki"]);
    expect(appsForNode(apps, "1").map((app) => app.name)).toStrictEqual(["crm"]);
    expect(appsForNode(apps, "9")).toStrictEqual([]);
  });
});

describe(learnedRoutes, () => {
  it("puts the domains in order and reads a null address list as none", () => {
    const rows = learnedRoutes({ "wiki.example.com": null, "crm.example.com": ["1.2.3.4"] });

    expect(rows).toStrictEqual([
      { domain: "crm.example.com", addresses: ["1.2.3.4"] },
      { domain: "wiki.example.com", addresses: [] },
    ]);
  });

  it("has nothing to show before the connector resolved anything", () => {
    expect(learnedRoutes({})).toStrictEqual([]);
  });
});

describe(domainCovers, () => {
  it("covers the domain itself, whatever its case or trailing dot", () => {
    expect(domainCovers("crm.example.com", "CRM.example.com.")).toBe(true);
    expect(domainCovers("crm.example.com", "api.crm.example.com")).toBe(false);
  });

  it("covers every name under a wildcard, not the bare domain", () => {
    expect(domainCovers("*.example.com", "crm.example.com")).toBe(true);
    expect(domainCovers("*.example.com", "api.crm.example.com")).toBe(true);
    expect(domainCovers("*.example.com", "example.com")).toBe(false);
    expect(domainCovers("*.example.com", "notexample.com")).toBe(false);
  });
});

describe(learnedForApp, () => {
  const answer = {
    "crm.example.com": ["1.2.3.4", "1.2.3.5"],
    "wiki.example.com": ["1.2.3.4", "9.9.9.9"],
    "api.crm.example.com": null,
  };

  it("counts only the addresses learned for the app's own domains", () => {
    expect(learnedForApp(["crm.example.com"], [answer])).toBe(2);
    expect(learnedForApp(["wiki.example.com"], [answer])).toBe(2);
  });

  it("counts an address once across domains and connectors", () => {
    expect(learnedForApp(["*.example.com"], [answer, answer])).toBe(3);
  });

  it("has nothing for an app no connector answered for", () => {
    expect(learnedForApp(["crm.example.com"], [])).toBe(0);
    expect(learnedForApp(["other.example.net"], [answer])).toBe(0);
  });
});
