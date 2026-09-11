import { describe, expect, it } from "vitest";

import type { AuditEvent } from "~/api/queries.ts";
import { bucketCount, bucketEvents } from "~/components/audit/stats.tsx";

const now = new Date("2026-09-11T12:00:00Z");
const hour = 60 * 60 * 1000;

function at(offsetHours: number, outcome = 200): AuditEvent {
  return {
    id: String(offsetHours),
    createdAt: new Date(now.getTime() - offsetHours * hour).toISOString(),
    actorKind: "local",
    actorUserId: "",
    actorName: "",
    action: "node.delete",
    targetKind: "node",
    targetId: "1",
    targetName: "",
    outcome,
    detail: {},
    remoteAddr: "",
  };
}

describe(bucketEvents, () => {
  it("has nothing to draw without events", () => {
    expect(bucketEvents([], now)).toHaveLength(0);
  });

  it("slices the window from the oldest event to now and counts the failures apart", () => {
    const buckets = bucketEvents([at(24, 500), at(1), at(0)], now);

    expect(buckets).toHaveLength(bucketCount);
    expect(buckets[0]).toMatchObject({ total: 1, failed: 1 });
    expect(buckets[bucketCount - 1]).toMatchObject({ total: 2, failed: 0 });
    expect(buckets.reduce((sum, bucket) => sum + bucket.total, 0)).toBe(3);
    expect(buckets[0]?.start.toISOString()).toBe("2026-09-10T12:00:00.000Z");
  });

  it("spreads a burst over a floor of one minute per bar instead of one bar", () => {
    const buckets = bucketEvents([at(0), at(0)], now);

    expect(buckets[bucketCount - 1]).toMatchObject({ total: 2 });
    expect(buckets[0]?.start.toISOString()).toBe("2026-09-11T11:36:00.000Z");
  });
});
