import { describe, expect, it } from "vitest";

import type { NodeClientWarning } from "~/api/schema.gen.ts";
import {
  connectivityNote,
  diagnosticFileName,
  severityLabel,
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

describe(severityLabel, () => {
  it("says the state in one word, whatever the client titled it", () => {
    expect(severityLabel("high")).toBe("Unhealthy");
    expect(severityLabel("medium")).toBe("Warning");
    expect(severityLabel("low")).toBe("Notice");
  });

  it("falls back to a word rather than showing a severity nobody knows", () => {
    expect(severityLabel("catastrophic")).toBe("Warning");
  });
});

describe(connectivityNote, () => {
  it("says so only when the client says traffic is affected", () => {
    expect(connectivityNote(warning())).toBeUndefined();
    expect(connectivityNote(warning({ impactsConnectivity: true }))).toBe(
      "The client says this affects connectivity.",
    );
  });
});

describe(diagnosticFileName, () => {
  it("names the fallback after the machine and the dump", () => {
    expect(diagnosticFileName("7", "netmap")).toBe("slopscale-netmap-7.txt");
  });
});
