import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import type {
  PostureIntegrationCheckBody,
  PostureIntegrationRequestBody,
} from "~/api/schema.gen.ts";
import type { Tone } from "~/components/ui/status.tsx";

export type PostureIntegrationStatus = "synced" | "failed" | "never" | "off";
export type PostureProviderType = PostureIntegrationRequestBody["provider"];

export interface IntegrationStatusInput {
  readonly enabled: boolean;
  readonly lastError: string;
  readonly lastSyncAt?: string | null | undefined;
}

const knownProviderTypes: readonly PostureProviderType[] = [
  "falcon",
  "sentinelone",
  "intune",
  "jamf",
  "kandji",
  "kolide",
];

export function toProviderType(value: string): PostureProviderType {
  const match = knownProviderTypes.find((item) => item === value);

  return match ?? "falcon";
}

/**
 * Maps an integration to its status: Off when disabled, Failed when lastError is set, Synced when
 * it has synced at least once, or Never synced.
 */
export function integrationStatus(integration: IntegrationStatusInput): PostureIntegrationStatus {
  if (!integration.enabled) {
    return "off";
  }

  if (integration.lastError !== "") {
    return "failed";
  }

  if (
    integration.lastSyncAt !== null &&
    integration.lastSyncAt !== undefined &&
    integration.lastSyncAt !== ""
  ) {
    return "synced";
  }

  return "never";
}

const statusLabels: Record<PostureIntegrationStatus, string> = {
  synced: "Synced",
  failed: "Failed",
  never: "Never synced",
  off: "Off",
};

export function statusLabel(status: PostureIntegrationStatus): string {
  return statusLabels[status];
}

const statusTones: Record<PostureIntegrationStatus, Tone> = {
  synced: "success",
  failed: "danger",
  never: "neutral",
  off: "neutral",
};

export function statusTone(status: PostureIntegrationStatus): Tone {
  return statusTones[status];
}

export function providerLabel(providers: readonly PostureProvider[], providerKey: string): string {
  const match = providers.find((item) => item.provider === providerKey);

  return match?.label ?? providerKey;
}

export function countIntegrations(count: number): string {
  return count === 1 ? "1 posture integration" : `${count} posture integrations`;
}

export interface Draft {
  readonly provider: PostureProviderType;
  readonly name: string;
  readonly enabled: boolean;
  readonly baseUrl: string;
  readonly clientId: string;
  readonly clientSecret: string;
  readonly apiToken: string;
  readonly tenantId: string;
}

export function draftFrom(
  integration?: PostureIntegration,
  defaultProvider?: PostureProvider,
): Draft {
  if (integration !== undefined) {
    return {
      provider: integration.provider,
      name: integration.name,
      enabled: integration.enabled,
      baseUrl: integration.config.baseUrl ?? "",
      clientId: integration.config.clientId ?? "",
      clientSecret: "",
      apiToken: "",
      tenantId: integration.config.tenantId ?? "",
    };
  }

  return {
    provider: toProviderType(defaultProvider?.provider ?? "falcon"),
    name: "",
    enabled: true,
    baseUrl: defaultProvider?.baseUrlDefault ?? "",
    clientId: "",
    clientSecret: "",
    apiToken: "",
    tenantId: "",
  };
}

export function isSecretField(field: string): boolean {
  return field === "clientSecret" || field === "apiToken";
}

export function isFieldRequired(field: string, editing: boolean, hasSecret: boolean): boolean {
  if (isSecretField(field) && editing && hasSecret) {
    return false;
  }

  return true;
}

export function fieldInputType(field: string): string {
  if (isSecretField(field)) {
    return "password";
  }
  if (field === "baseUrl") {
    return "url";
  }

  return "text";
}

export function fieldPlaceholder(
  field: string,
  baseUrlDefault: string | undefined,
  keeps: boolean,
): string {
  if (keeps) {
    return "Stored, leave empty to keep";
  }
  if (field === "baseUrl" && baseUrlDefault !== undefined) {
    return baseUrlDefault;
  }

  return "";
}

const fieldLabels: Record<string, string> = {
  baseUrl: "Base URL",
  clientId: "Client ID",
  clientSecret: "Client secret",
  apiToken: "API token",
  tenantId: "Tenant ID",
};

export function fieldLabel(field: string): string {
  return fieldLabels[field] ?? field;
}

export function getDraftFieldValue(draft: Draft, field: string): string {
  if (field === "baseUrl") {
    return draft.baseUrl;
  }
  if (field === "clientId") {
    return draft.clientId;
  }
  if (field === "clientSecret") {
    return draft.clientSecret;
  }
  if (field === "apiToken") {
    return draft.apiToken;
  }
  if (field === "tenantId") {
    return draft.tenantId;
  }

  return "";
}

export interface DraftIssueOptions {
  readonly editing: boolean;
  readonly hasSecret: boolean;
}

export function draftIssue(
  draft: Draft,
  provider: PostureProvider | undefined,
  options: DraftIssueOptions,
): string | null {
  if (draft.name.trim() === "") {
    return "Name is required";
  }

  if (provider === undefined) {
    return "Provider is required";
  }

  for (const field of provider.fields) {
    if (isFieldRequired(field, options.editing, options.hasSecret)) {
      const value = getDraftFieldValue(draft, field);

      if (value.trim() === "") {
        return `${fieldLabel(field)} is required`;
      }
    }
  }

  return null;
}

export function toRequestBody(
  draft: Draft,
  provider: PostureProvider | undefined,
): PostureIntegrationRequestBody {
  const fields = new Set(provider?.fields);
  const body: PostureIntegrationRequestBody = {
    provider: draft.provider,
    name: draft.name.trim(),
    enabled: draft.enabled,
  };

  if (fields.has("baseUrl") && draft.baseUrl.trim() !== "") {
    body.baseUrl = draft.baseUrl.trim();
  }

  if (fields.has("clientId") && draft.clientId.trim() !== "") {
    body.clientId = draft.clientId.trim();
  }

  if (fields.has("clientSecret") && draft.clientSecret.trim() !== "") {
    body.clientSecret = draft.clientSecret.trim();
  }

  if (fields.has("apiToken") && draft.apiToken.trim() !== "") {
    body.apiToken = draft.apiToken.trim();
  }

  if (fields.has("tenantId") && draft.tenantId.trim() !== "") {
    body.tenantId = draft.tenantId.trim();
  }

  return body;
}

export function toCheckBody(
  draft: Draft,
  provider: PostureProvider | undefined,
  id?: string,
): PostureIntegrationCheckBody {
  const body: PostureIntegrationCheckBody = toRequestBody(draft, provider);

  if (id !== undefined && id !== "") {
    body.id = id;
  }

  return body;
}
