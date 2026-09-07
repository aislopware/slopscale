import { describe, expect, it } from "vitest";

import {
  deliveryLabel,
  deliveryState,
  providerLabel,
  toChoice,
  toProvider,
  urlError,
  urlHost,
} from "~/components/webhooks/model.ts";

describe(deliveryState, () => {
  it("is never before any delivery", () => {
    expect(deliveryState({ lastDeliveryStatus: "" })).toBe("never");
  });

  it("treats a 2xx as ok", () => {
    expect(deliveryState({ lastDeliveryStatus: "204" })).toBe("ok");
  });

  it("treats other codes and error text as failed", () => {
    expect(deliveryState({ lastDeliveryStatus: "500" })).toBe("failed");
    expect(deliveryState({ lastDeliveryStatus: "dial tcp: connection refused" })).toBe("failed");
  });
});

describe(deliveryLabel, () => {
  it("names the code", () => {
    expect(deliveryLabel({ lastDeliveryStatus: "204" })).toBe("HTTP 204");
  });

  it("shortens long error text", () => {
    const long = "x".repeat(60);

    expect(deliveryLabel({ lastDeliveryStatus: long })).toBe(`${"x".repeat(40)}…`);
  });

  it("says never", () => {
    expect(deliveryLabel({ lastDeliveryStatus: "" })).toBe("Never delivered");
  });
});

describe("providers", () => {
  it("maps server values to choices and back", () => {
    expect(toChoice("slack")).toBe("slack");
    expect(toChoice("pager")).toBe("generic");
    expect(toChoice("")).toBe("generic");
    expect(toProvider("generic")).toBe("");
    expect(toProvider("discord")).toBe("discord");
  });

  it("labels providers", () => {
    expect(providerLabel("")).toBe("Generic (signed JSON)");
    expect(providerLabel("discord")).toBe("Discord");
  });
});

describe("urls", () => {
  it("extracts the host", () => {
    expect(urlHost("https://hooks.example.com/a/b")).toBe("hooks.example.com");
    expect(urlHost("nonsense")).toBe("nonsense");
  });

  it("accepts http and https only", () => {
    expect(urlError("")).toBeNull();
    expect(urlError("https://example.com/hook")).toBeNull();
    expect(urlError("ftp://example.com")).toBe("The URL must start with http:// or https://");
    expect(urlError("nope")).toBe("Enter a full URL, like https://example.com/hook");
  });
});
