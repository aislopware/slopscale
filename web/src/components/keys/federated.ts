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

/**
 * Converts editor entries to an object map of claim -> value. Both sides go in verbatim: the server
 * compares the claim against the token exactly, so trimming here would quietly change what the
 * operator asked for. `Object.fromEntries` rather than assignment into `{}`, because a claim named
 * `__proto__` or `constructor` assigned into an object literal is swallowed by the prototype.
 */
export function claimRulesToRecord(entries: readonly ClaimRuleEntry[]): Record<string, string> {
  return Object.fromEntries(
    entries
      .filter((entry) => entry.claim.trim() !== "" && entry.value.trim() !== "")
      .map((entry) => [entry.claim, entry.value]),
  );
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

/**
 * Validates that every row has both a non-empty claim and value, and no duplicate keys. A blank row
 * is what an operator left behind, so emptiness is judged on the trimmed text; two claims are the
 * same claim only when they are the same string, since that is how the server compares them.
 */
export function validateClaimRules(entries: readonly ClaimRuleEntry[]): string | undefined {
  const seen = new Set<string>();
  for (const entry of entries) {
    if (entry.claim.trim() === "" || entry.value.trim() === "") {
      return "Both claim and value are required for each rule.";
    }
    if (seen.has(entry.claim)) {
      return `Duplicate claim rule "${entry.claim}".`;
    }
    seen.add(entry.claim);
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

/**
 * Compares two claim maps by their own entries rather than by key lookup: a rule named `__proto__`
 * is an own property of both records, and reading it back with `record[key]` would answer with the
 * prototype instead of the rule.
 */
function recordsEqual(first: Record<string, string>, second: Record<string, string>): boolean {
  const entriesFirst = sortedEntries(first);
  const entriesSecond = sortedEntries(second);
  if (entriesFirst.length !== entriesSecond.length) {
    return false;
  }
  return entriesFirst.every(
    ([claim, value], idx) => claim === entriesSecond[idx]?.[0] && value === entriesSecond[idx]?.[1],
  );
}

function sortedEntries(record: Record<string, string>): [string, string][] {
  return Object.entries(record).toSorted(([first], [second]) => first.localeCompare(second));
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

/** EncodeURIComponent leaves these alone; the audience goes in whole, so they go with it. */
const alsoEncoded: Record<string, string> = {
  "!": "%21",
  "'": "%27",
  "(": "%28",
  ")": "%29",
  "*": "%2A",
};

/** A value safe to paste into a query string, whatever the operator typed in the audience field. */
function encodeQueryValue(value: string): string {
  return encodeURIComponent(value).replaceAll(
    /[!'()*]/gu,
    (character) => alsoEncoded[character] ?? character,
  );
}

/** A value inside single quotes, where the only character the shell still reads is the quote. */
function singleQuote(value: string): string {
  return `'${value.replaceAll("'", String.raw`'\''`)}'`;
}

/**
 * Builds the GitHub Actions snippet that mints an OIDC token and trades it for an API token: the
 * `permissions` block the job needs, the step that asks the runner's token service for a JWT with
 * this identity's audience, and the step that exchanges it. It is one workflow fragment rather than
 * a shell line, because that is where it is pasted; the audience is percent-encoded into the
 * request URL and everything else is single-quoted, so an audience or a client id with a `&`, a `#`
 * or a quote in it still runs.
 */
export function githubActionsSnippet(
  audience: string,
  serverUrl: string,
  clientId: string,
): string {
  const base = normalizeServerUrl(serverUrl);

  return `permissions:
  id-token: write

steps:
  - name: Request an OIDC token
    run: |
      ID_TOKEN=$(curl -sSf \\
        -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \\
        "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=${encodeQueryValue(audience)}" | jq -r .value)
      echo "::add-mask::$ID_TOKEN"
      echo "ID_TOKEN=$ID_TOKEN" >> "$GITHUB_ENV"
  - name: Exchange it for an API token
    run: |
      curl -sSf -X POST ${singleQuote(`${base}/api/v2/oauth/token-exchange`)} \\
        -d client_id=${singleQuote(clientId)} \\
        --data-urlencode jwt="$ID_TOKEN"`;
}
