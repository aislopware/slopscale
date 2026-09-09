import type { OAuthClient } from "~/api/queries.ts";
import type { UpdateOAuthClientRequestBody } from "~/api/schema.gen.ts";
import type { KindFilter } from "~/components/keys/search.ts";

export interface ClaimRuleEntry {
  readonly id: string;
  readonly claim: string;
  readonly value: string;
}

let ruleCounter = 0;

function generateRuleId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  ruleCounter += 1;
  return `rule-${ruleCounter}`;
}

/** Appends an empty or initialized claim rule row to the editor state. */
export function addClaimRule(
  entries: readonly ClaimRuleEntry[],
  claim = "",
  value = "",
): ClaimRuleEntry[] {
  return [...entries, { id: generateRuleId(), claim, value }];
}

/** Removes one claim rule row by id from the editor state. */
export function removeClaimRule(entries: readonly ClaimRuleEntry[], id: string): ClaimRuleEntry[] {
  return entries.filter((entry) => entry.id !== id);
}

/** Updates claim name or expected value for a rule entry. */
export function updateClaimRule(
  entries: readonly ClaimRuleEntry[],
  id: string,
  patch: Partial<Pick<ClaimRuleEntry, "claim" | "value">>,
): ClaimRuleEntry[] {
  return entries.map((entry) => (entry.id === id ? { ...entry, ...patch } : entry));
}

/** Converts editor entries to an object map of claim -> value. */
export function claimRulesToRecord(entries: readonly ClaimRuleEntry[]): Record<string, string> {
  const record: Record<string, string> = {};
  for (const entry of entries) {
    const claim = entry.claim.trim();
    const value = entry.value.trim();
    if (claim !== "" && value !== "") {
      record[claim] = value;
    }
  }
  return record;
}

/** Converts a claim -> value record into editor row entries. */
export function recordToClaimRules(record?: Record<string, string> | null): ClaimRuleEntry[] {
  if (record === undefined || record === null) {
    return [];
  }
  return Object.entries(record).map(([claim, value]) => ({
    id: generateRuleId(),
    claim,
    value,
  }));
}

/** Validates that every row has both a non-empty claim and value, and no duplicate keys. */
export function validateClaimRules(entries: readonly ClaimRuleEntry[]): string | undefined {
  const seen = new Set<string>();
  for (const entry of entries) {
    const claim = entry.claim.trim();
    const value = entry.value.trim();
    if (claim === "" || value === "") {
      return "Both claim and value are required for each rule.";
    }
    if (seen.has(claim)) {
      return `Duplicate claim rule "${claim}".`;
    }
    seen.add(claim);
  }
  return undefined;
}

/** Formats subject format hints and examples depending on the issuer provider. */
export function subjectHint(issuer?: string): string {
  const lower = issuer?.toLowerCase() ?? "";
  if (lower.includes("github")) {
    return "GitHub Actions: repo:org/repo:ref:refs/heads/main";
  }
  if (lower.includes("gitlab")) {
    return "GitLab: project_path:group/project:ref_type:branch:ref:main";
  }
  return "GitHub Actions: repo:org/repo:ref:refs/heads/main · GitLab: project_path:group/project:ref_type:branch:ref:main";
}

/** Validates that an issuer URL is a valid full https URL with a hostname. */
export function validateIssuerUrl(issuer: string): string | undefined {
  const trimmed = issuer.trim();
  if (trimmed === "") {
    return "Enter an issuer URL.";
  }
  let url: URL;
  try {
    url = new URL(trimmed);
  } catch {
    return "Enter a full URL starting with https://";
  }
  if (url.protocol !== "https:") {
    return "The issuer URL must use https://";
  }
  if (url.hostname === "") {
    return "The issuer URL must include a host.";
  }
  return undefined;
}

/** Extracts the host from an issuer URL, or echoes the original string if invalid. */
export function issuerHost(issuer: string): string {
  const trimmed = issuer.trim();
  if (trimmed === "") {
    return "";
  }
  try {
    return new URL(trimmed).host;
  } catch {
    return trimmed;
  }
}

/** Checks whether a client matches the selected kind filter. */
export function matchesKind(
  client: { readonly keyType?: string | null },
  kind: KindFilter,
): boolean {
  if (kind === "all") {
    return true;
  }
  const clientKind = client.keyType === "federated" ? "federated" : "client";
  return clientKind === kind;
}

export interface OAuthClientDraft {
  readonly description: string;
  readonly scopes: readonly string[];
  readonly tags: readonly string[];
  readonly issuer?: string | undefined;
  readonly audience?: string | undefined;
  readonly subject?: string | undefined;
  readonly customClaimRules?: Record<string, string> | undefined;
}

function arraysEqual(first: readonly string[], second: readonly string[]): boolean {
  if (first.length !== second.length) {
    return false;
  }
  const sortedFirst = first.toSorted();
  const sortedSecond = second.toSorted();
  return sortedFirst.every((val, idx) => val === sortedSecond[idx]);
}

function recordsEqual(first: Record<string, string>, second: Record<string, string>): boolean {
  const keysFirst = Object.keys(first).toSorted();
  const keysSecond = Object.keys(second).toSorted();
  if (keysFirst.length !== keysSecond.length) {
    return false;
  }
  return keysFirst.every((key, idx) => key === keysSecond[idx] && first[key] === second[key]);
}

/** Computes the diff between the original client and the edited draft for PATCH. */
export function diffOAuthClient(
  original: OAuthClient,
  draft: OAuthClientDraft,
): UpdateOAuthClientRequestBody {
  const patch: UpdateOAuthClientRequestBody = {};

  if (draft.description.trim() !== (original.description ?? "")) {
    patch.description = draft.description.trim();
  }

  if (!arraysEqual(original.scopes, draft.scopes)) {
    patch.scopes = [...draft.scopes];
  }

  if (!arraysEqual(original.tags ?? [], draft.tags)) {
    patch.tags = [...draft.tags];
  }

  if (original.keyType === "federated") {
    if (draft.issuer !== undefined && draft.issuer.trim() !== (original.issuer ?? "")) {
      patch.issuer = draft.issuer.trim();
    }
    if (draft.audience !== undefined && draft.audience.trim() !== (original.audience ?? "")) {
      patch.audience = draft.audience.trim();
    }
    if (draft.subject !== undefined && draft.subject.trim() !== (original.subject ?? "")) {
      patch.subject = draft.subject.trim();
    }
    if (
      draft.customClaimRules !== undefined &&
      !recordsEqual(original.customClaimRules ?? {}, draft.customClaimRules)
    ) {
      patch.customClaimRules = draft.customClaimRules;
    }
  }

  return patch;
}

/** Removes trailing slashes from server URL, falling back to window.location.origin. */
export function normalizeServerUrl(url?: string): string {
  if (url === undefined || url === "") {
    if (typeof window !== "undefined" && window.location?.origin !== undefined) {
      return window.location.origin;
    }
    return "";
  }
  return url.replace(/\/+$/u, "");
}

/** Builds the curl command to trade an OIDC token for a v2 API token. */
export function tokenExchangeCommand(serverUrl: string, clientId: string): string {
  const base = normalizeServerUrl(serverUrl);
  return `curl -X POST ${base}/api/v2/oauth/token-exchange -d client_id=${clientId} -d jwt="$ID_TOKEN"`;
}

/** Builds the GitHub Actions configuration snippet to mint an OIDC token. */
export function githubActionsSnippet(audience: string): string {
  return `permissions:
  id-token: write

curl -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=${audience}"`;
}
