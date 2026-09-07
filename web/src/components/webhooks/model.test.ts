import { describe, expect, it } from "vitest";

import {
  countEvents,
  deliveryLabel,
  deliveryState,
  formatDuration,
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
    expect(urlHost("mailto:ops@example.com, sec@example.com")).toBe(
      "ops@example.com, sec@example.com",
    );
  });

  it("accepts http and https only", () => {
    expect(urlError("")).toBeNull();
    expect(urlError("https://example.com/hook")).toBeNull();
    expect(urlError("ftp://example.com")).toBe("The URL must start with http:// or https://");
    expect(urlError("nope")).toBe("Enter a full URL, like https://example.com/hook");
  });

  it("asks telegram for a chat and email for recipients", () => {
    expect(urlError("https://api.telegram.org/bot1:a/sendMessage", "telegram")).toBe(
      "Add the chat as a chat_id query parameter",
    );
    expect(
      urlError("https://api.telegram.org/bot1:a/sendMessage?chat_id=-1", "telegram"),
    ).toBeNull();
    expect(urlError("https://example.com", "email")).toBe(
      "Enter mailto: followed by the recipients",
    );
    expect(urlError("mailto:", "email")).toBe("Enter mailto: followed by the recipients");
    expect(urlError("mailto:ops@example.com", "email")).toBeNull();
  });
});

describe(formatDuration, () => {
  it("picks the unit by size", () => {
    expect(formatDuration(120)).toBe("120 ms");
    expect(formatDuration(2500)).toBe("2.5 s");
    expect(formatDuration(4000)).toBe("4 s");
    expect(formatDuration(72_000)).toBe("1m 12s");
  });
});

describe(countEvents, () => {
  it("pluralises", () => {
    expect(countEvents(1)).toBe("1 event");
    expect(countEvents(3)).toBe("3 events");
  });
});
