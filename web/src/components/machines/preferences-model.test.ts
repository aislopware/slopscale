import { describe, expect, it } from "vitest";

import type { NodePreferences } from "~/api/schema.gen.ts";
import {
  canSavePreferences,
  exitNodeLabel,
  hasPreferenceChanges,
  preferenceChanges,
} from "~/components/machines/preferences-model.ts";

const current: NodePreferences = {
  acceptDns: true,
  acceptRoutes: false,
  advertiseConnector: false,
  advertiseExitNode: false,
  advertiseRoutes: ["10.0.0.0/24", "192.168.1.0/24"],
  autoUpdateApply: false,
  autoUpdateCheck: true,
  exitNode: "",
  exitNodeAllowLanAccess: false,
  hostname: "laptop",
  postureChecking: false,
  runSsh: true,
  shieldsUp: false,
};

describe(preferenceChanges, () => {
  it("sends nothing when the draft is the answer it opened on", () => {
    const changes = preferenceChanges(current, current);

    expect(changes).toStrictEqual({});
    expect(hasPreferenceChanges(changes)).toBe(false);
  });

  it("sends only the fields the operator touched", () => {
    const changes = preferenceChanges({ ...current, acceptRoutes: true, runSsh: false }, current);

    expect(changes).toStrictEqual({ acceptRoutes: true, runSsh: false });
    expect(hasPreferenceChanges(changes)).toBe(true);
  });

  it("trims the text fields and takes an empty exit node as clearing it", () => {
    const changes = preferenceChanges(
      { ...current, hostname: "  desk  ", exitNode: " 100.64.0.9 " },
      current,
    );

    expect(changes).toStrictEqual({ hostname: "desk", exitNode: "100.64.0.9" });
  });

  it("ignores the order of the advertised routes, since the client masks and dedupes them", () => {
    const reordered = ["192.168.1.0/24", "10.0.0.0/24"];

    expect(preferenceChanges({ ...current, advertiseRoutes: reordered }, current)).toStrictEqual(
      {},
    );
  });

  // The dialog keeps the answer it opened on and diffs against that. Diffing against a fresher
  // answer would send back a switch the machine's own owner flipped while the dialog was open.
  it("sends nothing when only the machine's own answer moved on", () => {
    const draft = { ...current };
    const answeredAgain = { ...current, shieldsUp: true, hostname: "renamed-on-the-machine" };

    expect(preferenceChanges(draft, current)).toStrictEqual({});
    expect(preferenceChanges(draft, answeredAgain)).toStrictEqual({
      shieldsUp: false,
      hostname: "laptop",
    });
  });

  it("sends the whole list once a route is added or dropped", () => {
    const changes = preferenceChanges({ ...current, advertiseRoutes: ["10.0.0.0/24"] }, current);

    expect(changes).toStrictEqual({ advertiseRoutes: ["10.0.0.0/24"] });
  });
});

// An empty hostname is a preference, not a half-filled field: it tells the client to use the
// machine's own operating system name, which is what most machines run with.
describe(canSavePreferences, () => {
  it("saves a switch while the hostname field is empty, and sends only the switch", () => {
    const nameless = { ...current, hostname: "" };
    const changes = preferenceChanges({ ...nameless, shieldsUp: true }, nameless);

    expect(changes).toStrictEqual({ shieldsUp: true });
    expect(canSavePreferences(changes, false)).toBe(true);
  });

  it("sends the empty hostname as a change when the operator cleared it", () => {
    const changes = preferenceChanges({ ...current, hostname: "" }, current);

    expect(changes).toStrictEqual({ hostname: "" });
    expect(canSavePreferences(changes, false)).toBe(true);
  });

  it("holds the form while a list is half typed, and while nothing changed", () => {
    const changes = preferenceChanges({ ...current, acceptRoutes: true }, current);

    expect(canSavePreferences(changes, true)).toBe(false);
    expect(canSavePreferences({}, false)).toBe(false);
  });
});

describe(exitNodeLabel, () => {
  it("says None rather than showing an empty value", () => {
    expect(exitNodeLabel(current)).toBe("None");
    expect(exitNodeLabel({ ...current, exitNode: "100.64.0.9" })).toBe("100.64.0.9");
  });
});
