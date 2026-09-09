import { describe, expect, it } from "vitest";

import { auditExportUrl } from "~/api/queries.ts";
import type { AuditFilters } from "~/api/queries.ts";
import { auditExportFileName } from "~/components/audit/export.ts";

const everything: AuditFilters = { action: "", actorUserId: "", range: "all" };

function query(url: string): URLSearchParams {
  return new URLSearchParams(url.slice(url.indexOf("?")));
}

describe(auditExportUrl, () => {
  it("asks only for the format when nothing narrows the list", () => {
    expect(auditExportUrl(everything, "csv")).toBe("/api/v1/audit/export?format=csv");
    expect(auditExportUrl(everything, "json")).toBe("/api/v1/audit/export?format=json");
  });

  it("carries the action and the actor the page is filtering by", () => {
    const url = auditExportUrl({ action: "node.", actorUserId: "7", range: "all" }, "csv");

    expect(query(url).get("action")).toBe("node.");
    expect(query(url).get("actorUserId")).toBe("7");
    expect(query(url).has("since")).toBe(false);
  });

  it("resolves the range preset to a lower bound, an hour back for 1h", () => {
    const before = Date.now();
    const since = query(auditExportUrl({ ...everything, range: "1h" }, "csv")).get("since");
    const hour = 3_600_000;

    const at = Date.parse(String(since));

    expect(since).not.toBeNull();
    expect(at).toBeGreaterThanOrEqual(before - hour - 1000);
    expect(at).toBeLessThanOrEqual(Date.now() - hour + 1000);
  });
});

describe(auditExportFileName, () => {
  it("names the file after the day it was taken and the format", () => {
    const day = new Date(2026, 8, 8, 13, 30);

    expect(auditExportFileName("csv", day)).toBe("slopscale-audit-2026-09-08.csv");
    expect(auditExportFileName("json", day)).toBe("slopscale-audit-2026-09-08.json");
  });
});
