import { describe, expect, it } from "vitest";

import type { NodeClientWarning } from "~/api/schema.gen.ts";
import {
  diagnosticFileName,
  warningLabel,
  warningTone,
} from "~/components/machines/health-model.ts";

function warning(overrides: Partial<NodeClientWarning> = {}): NodeClientWarning {
  return {
    code: "warm-up",
    impactsConnectivity: false,
    severity: "medium",
    text: "The client is still starting.",
    title: "Starting up",
    ...overrides,
  };
}

describe(warningTone, () => {
  it("maps the client's severities onto the console's tones", () => {
    expect(warningTone("high")).toBe("danger");
    expect(warningTone("medium")).toBe("warning");
    expect(warningTone("low")).toBe("info");
  });

  it("shows a severity a newer client invented rather than hiding it", () => {
    expect(warningTone("catastrophic")).toBe("warning");
  });
});

describe(warningLabel, () => {
  it("leaves the title alone while traffic still flows", () => {
    expect(warningLabel(warning())).toBe("Starting up");
  });

  it("folds the connectivity note into the label instead of a second badge", () => {
    expect(warningLabel(warning({ impactsConnectivity: true }))).toBe(
      "Starting up · affects connectivity",
    );
  });
});

describe(diagnosticFileName, () => {
  it("names the fallback after the machine and the dump", () => {
    expect(diagnosticFileName("7", "netmap")).toBe("slopscale-netmap-7.txt");
  });
});
