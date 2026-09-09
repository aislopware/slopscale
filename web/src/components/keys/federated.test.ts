import { describe, expect, it } from "vitest";

import type { OAuthClient } from "~/api/queries.ts";
import {
  addClaimRule,
  claimRulesToRecord,
  diffOAuthClient,
  githubActionsSnippet,
  issuerHost,
  matchesKind,
  recordToClaimRules,
  removeClaimRule,
  subjectHint,
  tokenExchangeCommand,
  updateClaimRule,
  validateClaimRules,
  validateIssuerUrl,
} from "~/components/keys/federated.ts";

describe(subjectHint, () => {
  it("provides generic hints for an unspecified issuer", () => {
    const hint = subjectHint();
    expect(hint).toContain("repo:org/repo:ref:refs/heads/main");
    expect(hint).toContain("project_path:group/project:ref_type:branch:ref:main");
  });

  it("customises hints for GitHub", () => {
    const hint = subjectHint("https://token.actions.githubusercontent.com");
    expect(hint).toContain("GitHub Actions");
    expect(hint).not.toContain("GitLab");
  });

  it("customises hints for GitLab", () => {
    const hint = subjectHint("https://gitlab.com");
    expect(hint).toContain("GitLab");
    expect(hint).not.toContain("GitHub Actions");
  });
});

describe("claim rule editor state", () => {
  it("adds and updates rules", () => {
    const empty = addClaimRule([]);
    expect(empty).toHaveLength(1);
    expect(empty[0]?.claim).toBe("");

    const entry = { id: "test-rule-id", claim: "", value: "" };
    const updated = updateClaimRule([entry], "test-rule-id", {
      claim: "repository_owner",
      value: "aislopware",
    });
    expect(updated[0]?.claim).toBe("repository_owner");
    expect(updated[0]?.value).toBe("aislopware");
  });

  it("removes rules by id", () => {
    const entries = [
      { id: "rule-1", claim: "claim1", value: "val1" },
      { id: "rule-2", claim: "claim2", value: "val2" },
    ];
    const pruned = removeClaimRule(entries, "rule-1");
    expect(pruned).toHaveLength(1);
    expect(pruned[0]?.claim).toBe("claim2");
  });

  it("converts between records and row entries", () => {
    const record = { env: "prod", ref: "main" };
    const entries = recordToClaimRules(record);
    expect(entries).toHaveLength(2);

    const roundTrip = claimRulesToRecord(entries);
    expect(roundTrip).toStrictEqual(record);
  });

  // Assigned into an object literal, a rule named __proto__ sets the prototype and disappears; the
  // identity would then be created without the condition the operator asked for.
  it("keeps a claim named after a property of every object", () => {
    const rules = [
      { id: "1", claim: "__proto__", value: "allowed" },
      { id: "2", claim: "constructor", value: "also allowed" },
    ];

    expect(validateClaimRules(rules)).toBeUndefined();
    expect(JSON.stringify(claimRulesToRecord(rules))).toBe(
      '{"__proto__":"allowed","constructor":"also allowed"}',
    );
  });

  // The server compares the claim against the token exactly, so the spaces around a value are part
  // of what the operator asked for: trimming them would change the condition without an edit.
  it("keeps claims and values exactly as they were typed", () => {
    const record = { " workflow ": " Deploy " };
    const roundTrip = claimRulesToRecord(recordToClaimRules(record));

    expect(roundTrip).toStrictEqual(record);
    expect(JSON.stringify(roundTrip)).toBe('{" workflow ":" Deploy "}');
  });

  it("validates empty and duplicate rules", () => {
    const invalid = addClaimRule([], "claimOnly", "");
    expect(validateClaimRules(invalid)).toBe("Both claim and value are required for each rule.");

    const duplicate = [
      { id: "1", claim: "repo", value: "a" },
      { id: "2", claim: "repo", value: "b" },
    ];
    expect(validateClaimRules(duplicate)).toBe('Duplicate claim rule "repo".');

    const valid = [{ id: "1", claim: "repo", value: "a" }];
    expect(validateClaimRules(valid)).toBeUndefined();
  });
});

describe(validateIssuerUrl, () => {
  it("accepts valid https URLs", () => {
    expect(validateIssuerUrl("https://token.actions.githubusercontent.com")).toBeUndefined();
    expect(validateIssuerUrl("https://gitlab.com")).toBeUndefined();
  });

  it("rejects non-https URLs and empty inputs", () => {
    expect(validateIssuerUrl("")).toBe("Enter an issuer URL.");
    expect(validateIssuerUrl("http://token.actions.githubusercontent.com")).toBe(
      "The issuer URL must use https://",
    );
    expect(validateIssuerUrl("not-a-url")).toBe("Enter a full URL starting with https://");
  });
});

describe(issuerHost, () => {
  it("extracts hostname", () => {
    expect(issuerHost("https://token.actions.githubusercontent.com/v1")).toBe(
      "token.actions.githubusercontent.com",
    );
    expect(issuerHost("")).toBe("");
  });
});

describe("kind filter", () => {
  const clientRow: OAuthClient = {
    clientId: "c1",
    keyType: "client",
    description: "App",
    issuer: "",
    audience: "",
    subject: "",
    customClaimRules: {},
    scopes: ["dns"],
    tags: ["tag:ci"],
    userId: null,
    createdAt: null,
  };

  const federatedRow: OAuthClient = {
    clientId: "f1",
    keyType: "federated",
    description: "CI deploy",
    issuer: "https://token.actions.githubusercontent.com",
    audience: "https://scale.example.com",
    subject: "repo:org/repo:ref:refs/heads/main",
    customClaimRules: { workflow: "deploy" },
    scopes: ["devices:core"],
    tags: ["tag:server"],
    userId: null,
    createdAt: null,
  };

  it("matches client and all filter correctly", () => {
    expect(matchesKind(clientRow, "all")).toBe(true);
    expect(matchesKind(federatedRow, "all")).toBe(true);
    expect(matchesKind(clientRow, "client")).toBe(true);
    expect(matchesKind(federatedRow, "client")).toBe(false);
  });

  it("matches federated filter correctly", () => {
    expect(matchesKind(clientRow, "federated")).toBe(false);
    expect(matchesKind(federatedRow, "federated")).toBe(true);
  });
});

describe(diffOAuthClient, () => {
  const federatedClient: OAuthClient = {
    clientId: "f1",
    keyType: "federated",
    description: "Original description",
    issuer: "https://token.actions.githubusercontent.com",
    audience: "https://scale.example.com",
    subject: "repo:org/repo:ref:refs/heads/main",
    customClaimRules: { workflow: "deploy" },
    scopes: ["dns"],
    tags: ["tag:ci"],
    userId: null,
    createdAt: null,
  };

  // Opening the editor on a record and saving another field must not rewrite its claim rules.
  it("sends nothing for a claim rule nobody touched, whitespace and all", () => {
    const client: OAuthClient = {
      ...federatedClient,
      customClaimRules: { workflow: " Deploy " },
    };
    const rules = recordToClaimRules(client.customClaimRules);

    const patch = diffOAuthClient(client, {
      description: client.description,
      scopes: client.scopes,
      tags: client.tags,
      issuer: client.issuer,
      audience: client.audience,
      subject: client.subject,
      customClaimRules: claimRulesToRecord(rules),
    });

    expect(patch).toStrictEqual({});
  });

  it("returns an empty patch when unchanged", () => {
    const patch = diffOAuthClient(federatedClient, {
      description: "Original description",
      scopes: ["dns"],
      tags: ["tag:ci"],
      issuer: "https://token.actions.githubusercontent.com",
      audience: "https://scale.example.com",
      subject: "repo:org/repo:ref:refs/heads/main",
      customClaimRules: { workflow: "deploy" },
    });
    expect(patch).toStrictEqual({});
  });

  it("detects changed fields", () => {
    const patch = diffOAuthClient(federatedClient, {
      description: "New description",
      scopes: ["dns", "users:read"],
      tags: ["tag:ci"],
      issuer: "https://token.actions.githubusercontent.com",
      audience: "https://scale.example.com",
      subject: "repo:org/repo:ref:refs/heads/release",
      customClaimRules: { workflow: "deploy", env: "staging" },
    });
    expect(patch).toStrictEqual({
      description: "New description",
      scopes: ["dns", "users:read"],
      subject: "repo:org/repo:ref:refs/heads/release",
      customClaimRules: { workflow: "deploy", env: "staging" },
    });
  });

  // The dialog keeps the record it opened on and diffs against that. Diffing against the record as
  // the list refetched it would turn a field somebody else changed into a field this form sends.
  it("sends nothing when only the record behind the form moved on", () => {
    const draft = {
      description: federatedClient.description,
      scopes: federatedClient.scopes,
      tags: federatedClient.tags,
      issuer: federatedClient.issuer,
      audience: federatedClient.audience,
      subject: federatedClient.subject,
      customClaimRules: { workflow: "deploy" },
    };
    const refetched: OAuthClient = {
      ...federatedClient,
      description: "Renamed elsewhere",
      scopes: ["dns", "users:read"],
    };

    expect(diffOAuthClient(federatedClient, draft)).toStrictEqual({});
    expect(diffOAuthClient(refetched, draft)).toStrictEqual({
      description: "Original description",
      scopes: ["dns"],
    });
  });
});

describe("exchange and snippet helpers", () => {
  it("builds the exchange curl command", () => {
    const cmd = tokenExchangeCommand("https://scale.example.com", "id123");
    expect(cmd).toBe(
      'curl -X POST https://scale.example.com/api/v2/oauth/token-exchange -d client_id=id123 -d jwt="$ID_TOKEN"',
    );
  });

  it("builds a workflow fragment that mints a token and exchanges it", () => {
    const lines = githubActionsSnippet(
      "https://scale.example.com",
      "https://scale.example.com",
      "id123",
    ).split("\n");

    expect(lines).toStrictEqual([
      "permissions:",
      "  id-token: write",
      "",
      "steps:",
      "  - name: Request an OIDC token",
      "    run: |",
      "      ID_TOKEN=$(curl -sSf \\",
      '        -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \\',
      '        "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=https%3A%2F%2Fscale.example.com" | jq -r .value)',
      '      echo "::add-mask::$ID_TOKEN"',
      '      echo "ID_TOKEN=$ID_TOKEN" >> "$GITHUB_ENV"',
      "  - name: Exchange it for an API token",
      "    run: |",
      "      curl -sSf -X POST 'https://scale.example.com/api/v2/oauth/token-exchange' \\",
      "        -d client_id='id123' \\",
      '        --data-urlencode jwt="$ID_TOKEN"',
    ]);
  });

  // An audience with a & in it would end the query parameter, a # would start a fragment, and a
  // quote in a client id would close the string the shell is reading.
  it("encodes and quotes what an operator may have typed", () => {
    const snippet = githubActionsSnippet(
      "urn:example:a&b#c'd",
      "https://scale.example.com",
      "id'123",
    );

    expect(snippet).toContain('audience=urn%3Aexample%3Aa%26b%23c%27d"');
    expect(snippet).not.toContain("a&b");
    expect(snippet).toContain(String.raw`-d client_id='id'\''123'`);
  });
});
