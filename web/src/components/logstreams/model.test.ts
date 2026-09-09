import { describe, expect, it } from "vitest";

import {
  countersLabel,
  destinationLabel,
  destinationOption,
  statusLabel,
  streamState,
  urlError,
} from "~/components/logstreams/model.ts";

describe(streamState, () => {
  it("reports disabled before anything else", () => {
    expect(streamState({ enabled: false, lastDeliveryStatus: "200" })).toBe("disabled");
  });

  it("is never before any batch", () => {
    expect(streamState({ enabled: true, lastDeliveryStatus: "" })).toBe("never");
  });

  it("reads the last status", () => {
    expect(streamState({ enabled: true, lastDeliveryStatus: "200" })).toBe("ok");
    expect(streamState({ enabled: true, lastDeliveryStatus: "503" })).toBe("failed");
    expect(streamState({ enabled: true, lastDeliveryStatus: "dial tcp: refused" })).toBe("failed");
  });
});

describe(statusLabel, () => {
  it("shows a code or a short error", () => {
    expect(statusLabel({ lastDeliveryStatus: "" })).toBe("Never");
    expect(statusLabel({ lastDeliveryStatus: "200" })).toBe("HTTP 200");
    expect(statusLabel({ lastDeliveryStatus: "x".repeat(50) })).toBe(`${"x".repeat(40)}…`);
  });
});

describe(countersLabel, () => {
  it("mentions drops only when there are any", () => {
    expect(countersLabel({ delivered: 1204, dropped: 0 })).toBe("1,204 delivered");
    expect(countersLabel({ delivered: 5, dropped: 3 })).toBe("5 delivered · 3 dropped");
  });
});

describe("destinations", () => {
  it("labels the known ones and falls back to HTTP", () => {
    expect(destinationLabel("splunk")).toBe("Splunk");
    expect(destinationOption("nope").value).toBe("http");
    expect(destinationOption("datadog").tokenRequired).toBe(true);
  });
});

describe(urlError, () => {
  it("accepts http and https only", () => {
    expect(urlError("")).toBeNull();
    expect(urlError("https://loki.example.com/loki/api/v1/push")).toBeNull();
    expect(urlError("ftp://x")).toBe("The URL must start with http:// or https://");
    expect(urlError("nope")).toBe("Enter a full URL, like https://collector.example.com/logs");
  });
});
