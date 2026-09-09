import { describe, expect, it } from "vitest";

import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import {
  countIntegrations,
  draftFrom,
  draftIssue,
  fieldInputType,
  fieldLabel,
  fieldPlaceholder,
  getDraftFieldValue,
  integrationStatus,
  isFieldRequired,
  isSecretField,
  providerLabel,
  statusLabel,
  statusTone,
  toCheckBody,
  toProviderType,
  toRequestBody,
} from "~/components/posture-integrations/model.ts";
import type { Draft } from "~/components/posture-integrations/model.ts";

const mockProviders: readonly PostureProvider[] = [
  {
    provider: "falcon",
    label: "CrowdStrike Falcon",
    prefix: "falcon:",
    fields: ["baseUrl", "clientId", "clientSecret"],
    baseUrlDefault: "https://api.crowdstrike.com",
    attributes: [{ name: "falcon:ztaScore", type: "number", description: "ZTA score" }],
    help: "https://falcon.crowdstrike.com",
  },
  {
    provider: "sentinelone",
    label: "SentinelOne",
    prefix: "sentinelOne:",
    fields: ["baseUrl", "apiToken"],
    attributes: [
      { name: "sentinelOne:infected", type: "boolean", description: "Infection status" },
    ],
    help: "https://sentinelone.com",
  },
  {
    provider: "kolide",
    label: "Kolide",
    prefix: "kolide:",
    fields: ["apiToken"],
    baseUrlDefault: "https://api.kolide.com",
    attributes: [{ name: "kolide:authState", type: "string", description: "Auth state" }],
    help: "https://kolide.com",
  },
];

const [falconProvider] = mockProviders;

describe(integrationStatus, () => {
  it("is off when disabled regardless of sync state or errors", () => {
    expect(integrationStatus({ enabled: false, lastError: "", lastSyncAt: null })).toBe("off");
    expect(
      integrationStatus({
        enabled: false,
        lastError: "unauthorized",
        lastSyncAt: "2026-01-01T00:00:00Z",
      }),
    ).toBe("off");
  });

  it("is failed when enabled and lastError is not empty", () => {
    expect(
      integrationStatus({
        enabled: true,
        lastError: "upstream timeout",
        lastSyncAt: "2026-01-01T00:00:00Z",
      }),
    ).toBe("failed");
  });

  it("is synced when enabled, error is empty, and lastSyncAt is present", () => {
    expect(
      integrationStatus({
        enabled: true,
        lastError: "",
        lastSyncAt: "2026-01-01T00:00:00Z",
      }),
    ).toBe("synced");
  });

  it("is never when enabled, error is empty, and lastSyncAt is missing", () => {
    expect(integrationStatus({ enabled: true, lastError: "", lastSyncAt: null })).toBe("never");
    expect(integrationStatus({ enabled: true, lastError: "", lastSyncAt: undefined })).toBe(
      "never",
    );
    expect(integrationStatus({ enabled: true, lastError: "", lastSyncAt: "" })).toBe("never");
  });
});

describe("statusLabel and statusTone", () => {
  it("returns human-readable status labels", () => {
    expect(statusLabel("synced")).toBe("Synced");
    expect(statusLabel("failed")).toBe("Failed");
    expect(statusLabel("never")).toBe("Never synced");
    expect(statusLabel("off")).toBe("Off");
  });

  it("returns appropriate tones for status", () => {
    expect(statusTone("synced")).toBe("success");
    expect(statusTone("failed")).toBe("danger");
    expect(statusTone("never")).toBe("neutral");
    expect(statusTone("off")).toBe("neutral");
  });
});

describe(providerLabel, () => {
  it("returns the catalog label when found", () => {
    expect(providerLabel(mockProviders, "falcon")).toBe("CrowdStrike Falcon");
    expect(providerLabel(mockProviders, "sentinelone")).toBe("SentinelOne");
  });

  it("falls back to the provider key when not found", () => {
    expect(providerLabel(mockProviders, "unknownProvider")).toBe("unknownProvider");
  });
});

describe(countIntegrations, () => {
  it("pluralises correctly", () => {
    expect(countIntegrations(1)).toBe("1 posture integration");
    expect(countIntegrations(0)).toBe("0 posture integrations");
    expect(countIntegrations(5)).toBe("5 posture integrations");
  });
});

describe(toProviderType, () => {
  it("matches common provider types", () => {
    expect(toProviderType("falcon")).toBe("falcon");
    expect(toProviderType("sentinelone")).toBe("sentinelone");
    expect(toProviderType("intune")).toBe("intune");
    expect(toProviderType("jamf")).toBe("jamf");
  });

  it("matches other provider types with fallback", () => {
    expect(toProviderType("kandji")).toBe("kandji");
    expect(toProviderType("kolide")).toBe("kolide");
    expect(toProviderType("unknown")).toBe("falcon");
  });
});

describe("draft creation", () => {
  it("creates a default draft from default provider", () => {
    const draft = draftFrom(undefined, falconProvider);
    expect(draft).toStrictEqual({
      provider: "falcon",
      name: "",
      enabled: true,
      baseUrl: "https://api.crowdstrike.com",
      clientId: "",
      clientSecret: "",
      apiToken: "",
      tenantId: "",
    });
  });

  it("creates draft from an existing integration leaving secrets empty", () => {
    const existing: PostureIntegration = {
      id: "10",
      name: "Existing Falcon",
      provider: "falcon",
      prefix: "falcon:",
      enabled: false,
      config: {
        baseUrl: "https://custom.falcon.com",
        clientId: "client-123",
      },
      hasSecret: true,
      lastError: "",
      lastMatched: 12,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
    };

    const draft = draftFrom(existing, falconProvider);
    expect(draft).toStrictEqual({
      provider: "falcon",
      name: "Existing Falcon",
      enabled: false,
      baseUrl: "https://custom.falcon.com",
      clientId: "client-123",
      clientSecret: "",
      apiToken: "",
      tenantId: "",
    });
  });
});

describe("fields logic", () => {
  it("identifies secret fields", () => {
    expect(isSecretField("clientSecret")).toBe(true);
    expect(isSecretField("apiToken")).toBe(true);
    expect(isSecretField("baseUrl")).toBe(false);
    expect(isSecretField("clientId")).toBe(false);
    expect(isSecretField("tenantId")).toBe(false);
  });

  it("requires non-secret fields always", () => {
    expect(isFieldRequired("baseUrl", false, false)).toBe(true);
    expect(isFieldRequired("baseUrl", true, true)).toBe(true);
    expect(isFieldRequired("clientId", true, true)).toBe(true);
  });

  it("handles secret fields on edit", () => {
    expect(isFieldRequired("clientSecret", false, false)).toBe(true);
    expect(isFieldRequired("clientSecret", true, false)).toBe(true);
    expect(isFieldRequired("clientSecret", true, true)).toBe(false);
    expect(isFieldRequired("apiToken", true, true)).toBe(false);
  });

  it("returns labels for credentials", () => {
    expect(fieldLabel("clientSecret")).toBe("Client secret");
    expect(fieldLabel("apiToken")).toBe("API token");
    expect(fieldLabel("other")).toBe("other");
  });

  it("returns labels for connection fields", () => {
    expect(fieldLabel("baseUrl")).toBe("Base URL");
    expect(fieldLabel("clientId")).toBe("Client ID");
    expect(fieldLabel("tenantId")).toBe("Tenant ID");
  });

  it("extracts url and identity fields from draft", () => {
    const draft: Draft = {
      provider: "falcon",
      name: "Test",
      enabled: true,
      baseUrl: "https://example.com",
      clientId: "cid",
      clientSecret: "sec",
      apiToken: "tok",
      tenantId: "tid",
    };

    expect(getDraftFieldValue(draft, "baseUrl")).toBe("https://example.com");
    expect(getDraftFieldValue(draft, "clientId")).toBe("cid");
    expect(getDraftFieldValue(draft, "tenantId")).toBe("tid");
    expect(getDraftFieldValue(draft, "unknown")).toBe("");
  });

  it("extracts secret fields from draft", () => {
    const draft: Draft = {
      provider: "falcon",
      name: "Test",
      enabled: true,
      baseUrl: "https://example.com",
      clientId: "cid",
      clientSecret: "sec",
      apiToken: "tok",
      tenantId: "tid",
    };

    expect(getDraftFieldValue(draft, "clientSecret")).toBe("sec");
    expect(getDraftFieldValue(draft, "apiToken")).toBe("tok");
  });

  it("determines field input type and placeholder", () => {
    expect(fieldInputType("clientSecret")).toBe("password");
    expect(fieldInputType("baseUrl")).toBe("url");
    expect(fieldInputType("clientId")).toBe("text");

    expect(fieldPlaceholder("clientSecret", undefined, true)).toBe("Stored, leave empty to keep");
    expect(fieldPlaceholder("baseUrl", "https://default.com", false)).toBe("https://default.com");
  });
});

describe(draftIssue, () => {
  it("requires a name", () => {
    const draft: Draft = {
      provider: "falcon",
      name: "  ",
      enabled: true,
      baseUrl: "https://api.crowdstrike.com",
      clientId: "id",
      clientSecret: "secret",
      apiToken: "",
      tenantId: "",
    };

    expect(draftIssue(draft, falconProvider, { editing: false, hasSecret: false })).toBe(
      "Name is required",
    );
  });

  it("requires a provider", () => {
    const draft: Draft = {
      provider: "falcon",
      name: "Falcon",
      enabled: true,
      baseUrl: "https://api.crowdstrike.com",
      clientId: "id",
      clientSecret: "secret",
      apiToken: "",
      tenantId: "",
    };

    expect(draftIssue(draft, undefined, { editing: false, hasSecret: false })).toBe(
      "Provider is required",
    );
  });

  it("requires provider-specific fields on create", () => {
    const draft: Draft = {
      provider: "falcon",
      name: "Falcon",
      enabled: true,
      baseUrl: "https://api.crowdstrike.com",
      clientId: "",
      clientSecret: "secret",
      apiToken: "",
      tenantId: "",
    };

    expect(draftIssue(draft, falconProvider, { editing: false, hasSecret: false })).toBe(
      "Client ID is required",
    );
  });

  it("allows empty secret when editing an integration with stored secret", () => {
    const draft: Draft = {
      provider: "falcon",
      name: "Falcon",
      enabled: true,
      baseUrl: "https://api.crowdstrike.com",
      clientId: "client-id",
      clientSecret: "",
      apiToken: "",
      tenantId: "",
    };

    expect(draftIssue(draft, falconProvider, { editing: true, hasSecret: true })).toBeNull();
  });
});

describe("payload formatting", () => {
  it("formats create request body keeping only provider fields", () => {
    const draft: Draft = {
      provider: "falcon",
      name: " My Falcon ",
      enabled: true,
      baseUrl: " https://api.crowdstrike.com ",
      clientId: " cid ",
      clientSecret: " secret ",
      apiToken: " unused ",
      tenantId: " unused ",
    };

    const body = toRequestBody(draft, falconProvider);
    expect(body).toStrictEqual({
      provider: "falcon",
      name: "My Falcon",
      enabled: true,
      baseUrl: "https://api.crowdstrike.com",
      clientId: "cid",
      clientSecret: "secret",
    });
  });

  it("formats check request body including id when editing", () => {
    const draft: Draft = {
      provider: "falcon",
      name: "Falcon",
      enabled: true,
      baseUrl: "https://api.crowdstrike.com",
      clientId: "cid",
      clientSecret: "",
      apiToken: "",
      tenantId: "",
    };

    const createCheck = toCheckBody(draft, falconProvider);
    expect(createCheck.id).toBeUndefined();

    const editCheck = toCheckBody(draft, falconProvider, "42");
    expect(editCheck.id).toBe("42");
  });
});
