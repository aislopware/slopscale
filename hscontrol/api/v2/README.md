# API v2: Slopscale's v2 API

This is Slopscale's v2 HTTP API, served at `/api/v2`. Some of its endpoints are
**ported from Tailscale's API**, reusing Tailscale's wire shapes, so the
Tailscale ecosystem that cannot talk to Slopscale today works: the
[Terraform/OpenTofu provider], [tscli], and the official [Go client]
(`tailscale.com/client/tailscale/v2`).

It is **not** a port of the whole Tailscale API. Ported endpoints are added one
at a time, only as we need them; a slopscale-native v2 endpoint may use
slopscale's own conventions. The slopscale-native admin API stays at `/api/v1`
(`hscontrol/api/v1`). This guide is for the endpoints **ported from Tailscale**.

[Terraform/OpenTofu provider]: https://registry.terraform.io/providers/tailscale/tailscale/latest
[tscli]: https://github.com/jaxxstorm/tscli
[Go client]: https://pkg.go.dev/tailscale.com/client/tailscale/v2

## Conventions

- Operations derived from Tailscale carry the `Tailscale compat` tag.
- The `{tailnet}` path segment must be `-` (the single Slopscale tailnet);
  anything else is `404`. See `requireDefaultTailnet`.
- Errors use **Tailscale's** body (`{"message","data","status"}`), installed as
  a per-API transform (`tailscaleErrorTransformer` in `errors.go`). A future
  slopscale-native v2 operation would keep Huma's RFC 9457 problem+json.
- Auth accepts a credential as **HTTP Basic** (key as username, what the SDK
  sends) or **Bearer**: an admin API key (`hskey-api-…`), or an OAuth access
  token (`hskey-oauthtok-…`). See `authMiddleware`.
- Each operation declares the Tailscale scope it requires (`auth_keys`,
  `oauth_keys`, `devices:core`, `devices:routes`,
  `devices:posture_attributes`, `policy_file`, `feature_settings`, `users`,
  `dns`, `webhooks`, each with a `:read` subset, plus `all`/`all:read`, and
  the read-only `logs:configuration:read`).
  `requireScope` records it both for the middleware and in the generated
  OpenAPI, as an `x-required-scope` extension and a sentence in the operation
  description, so the scope shows up in the docs and spec. Enforcement: an
  **admin API key is all-access** (scope checks skipped); an **OAuth access
  token is scope-limited**, the middleware checks the operation's declared scope
  against the token's grant (`scope.Grants`, where a write scope subsumes its
  `:read` and `all`/`all:read` are super-scopes). The two are told apart by
  credential prefix.
- Resolve one entity by id with a typed getter (`GetNodeByID`, `GetUserByID`,
  `GetAPIKeyByID`, `GetPreAuthKeyByID`); add one to state/db if it is missing
  rather than scanning a `List`. Build responses from the view accessors
  (`NodeView`/`UserView`/`PreAuthKeyView`), never `AsStruct()`.
- Webhooks (`/tailnet/{tailnet}/webhooks`, `/webhooks/{endpointId}` with
  `/test` and `/rotate`) use Tailscale's field names (`endpointId`,
  `endpointUrl`, `created`, `lastModified`, `creatorLoginName`) so the Go
  client's `WebhooksResource` works unchanged; the secret appears only in the
  create and rotate responses.
- Reuse upstream wire shapes, but declare the request/response structs here:
  Huma reflects these to build the OpenAPI schema, and the upstream `Key`'s
  `ExpirySeconds *time.Duration` marshals as nanoseconds, which the spec and
  every client read as seconds.

## OAuth clients & scopes

Most of the Tailscale ecosystem (the Terraform provider, `tscli`, the Go client)
accepts **either** an API key **or OAuth 2.0 client-credentials**; the Kubernetes
operator is OAuth-only. Supporting OAuth lets all of them drive Slopscale.

- **OAuth clients** are not a separate resource; they are `keyType:"client"` on
  the keys endpoint, exactly as Tailscale does it. Create
  (`POST /api/v2/tailnet/-/keys` with `{"keyType":"client","scopes":[…],"tags":[…]}`)
  returns a `Key` whose `id` is the client id and whose `key` is the secret,
  **shown once**; get/list never re-expose it. The secret is
  `hskey-client-<clientID>-<secret>`, embedding the client id so the token
  endpoint derives it from the secret (Tailscale's `get-authkey` trick). See
  `keys.go` (`createOAuthClient`) and `db/oauth.go`.
- **Token endpoint** `POST /api/v2/oauth/token` (`oauth.go`) is a plain handler,
  not a Huma operation: it takes `application/x-www-form-urlencoded` and emits
  RFC 6749 OAuth2 error bodies (`{"error","error_description"}`). Credentials
  arrive in the body or HTTP Basic; optional space-delimited `scope`/`tags`
  narrow the token to a subset of the client's grant. It returns a 1-hour
  `Bearer` access token (`hskey-oauthtok-…`).
- **Scope enforcement** is the one seam in `authMiddleware`. **Tag enforcement**:
  an auth key minted by a token may only carry tags the token holds, or tags
  owned-by them via the policy `tagOwners` (`State.TagOwnedByTags` →
  `policy/v2`), so e.g. an operator token tagged `tag:k8s-operator` may mint
  `tag:k8s` keys.
- Credentials/tokens are stored like API keys: a public id/prefix plus an
  **Argon2id** hash of the secret (no JWT, no signing keys). `OAuthClient` and
  `OAuthAccessToken` live in `types/oauth.go` and `db/oauth.go`.
- **Updating a client** is `PUT /api/v2/tailnet/-/keys/{keyId}` with
  `{"keyType":"client","scopes":[…],"tags":[…],"description":"…"}`
  (`keys_update.go`), which the provider's `tailscale_oauth_client` resource
  uses on an in-place change. It replaces the grant wholesale and never
  re-exposes the secret; the same tag-ownership rules as create apply
  (`authorizeClientGrant`).

## Federated identities

`keyType:"federated"` on the keys endpoint registers a **workload identity**
(`federated.go`): a CI job or cloud workload presents the OIDC JWT its own
platform signed and gets a Slopscale access token back, so no long-lived secret
has to be stored anywhere.

- Create/update with `{"keyType":"federated","scopes":[…],"tags":[…],
"description":"…","audience":"…","issuer":"https://…","subject":"…",
"customClaimRules":{"claim":"value"}}`. Issuer, audience and subject are
  required; the issuer must be an `http(s)` URL that passes the egress guard.
- Exchange at `POST /api/v2/oauth/token-exchange` (`token_exchange.go`), a
  plain form handler like the token endpoint: `grant_type` (optional; the Go
  client omits it), `client_id` and `jwt`. It verifies the signature against
  the issuer's JWKS via `github.com/coreos/go-oidc/v3`, checks `iss`, `aud`,
  `sub`, `exp`, `nbf` and `iat` with 60s of leeway, then every custom claim
  rule (a list-valued claim satisfies a rule when it contains the value), and
  mints a one-hour access token carrying the identity's scopes and tags.
- Discovery documents are cached per issuer for an hour, bounded to 32 issuers,
  and fetched through `egress.Transport()` because the issuer is operator
  input. Every exchange, refused or not, is audited as `oauth.token.exchange`.

## Endpoints beyond the core

- **Policy validation** `POST /api/v2/tailnet/-/acl/validate` (`acl_validate.go`).
  The provider's `tailscale_acl` plan modifier calls it on every plan. The body
  is either a policy (JSON or HuJSON), which is compiled without being stored,
  or `{"tests":[…]}` / a bare `[…]` list of ACL tests, which run against the
  **policy in force**. Both answer `200`: an empty `message` means it passed, a
  non-empty one carries the failure and the per-test detail in `data`.
- **Raw policy** `GET /api/v2/tailnet/-/acl` with `Accept: application/hujson`
  returns the stored bytes untouched with an `ETag`, so a conditional write
  round-trips comments and trailing commas.
- **Whole-tailnet DNS** `GET`/`POST /api/v2/tailnet/-/dns/configuration`
  (`dns_configuration.go`) is what `tailscale_dns_configuration` reads and
  writes in one call. `useWithExitNode` and `overrideLocalDNS` are honoured
  (they exist in `types.DNSSettings`); `magicDNS` comes from the config file, so
  a POST may only repeat the value in force. The POST replaces every aspect at
  once, so unlike the single-aspect endpoints it never prunes a flag silently —
  an inconsistent request is a `400` naming the problem.
  The single-aspect `dns/nameservers` body carries **only** `dns`: Tailscale's
  clients decode that response into a `map[string][]string`, so any extra
  member breaks them. Read `overrideLocalDNS` and `magicDNS` from
  `dns/configuration` and `dns/preferences`.
- **Posture integrations** `GET`/`POST /api/v2/tailnet/-/posture/integrations`
  and `GET`/`PATCH`/`DELETE /api/v2/posture/integrations/{id}`
  (`posture_integrations.go`) wire Tailscale's
  `{id, provider, cloudId, clientId, tenantId, clientSecret}` onto
  `types.PostureIntegration`. Tailscale's provider names are mapped
  (`jamfpro` ↔ slopscale's `jamf`, a CrowdStrike `cloudId` region such as
  `us-1` onto the Falcon API host); a provider slopscale has no integration for
  is a `400` naming it. The secret is write-only. Scope
  `devices:posture_attributes`.
- **Log streaming** `GET`/`PUT`/`DELETE /api/v2/tailnet/-/logging/{logType}/stream`
  (`logging.go`). `configuration` maps onto the audit log streams; `network` is
  a `404` saying network flow logs are not available on slopscale, because the
  client, not the control server, produces them. `destinationType` must be one
  of `types.LogStreamDestinations` (http, splunk, elastic, datadog, axiom,
  loki); anything else is a `400` listing the supported ones, as are the S3 and
  GCS fields. The API owns exactly one stream, named
  `tailscale-api:configuration`, so a `PUT` replaces rather than adds. Scopes
  `logs:configuration:read` and `logs:configuration`.
- **`fields=all` on a device** (`devices_fields.go`) adds
  `blocksIncomingConnections`, `isExternal` (always false: slopscale has no
  shared-in devices), `connectedToControl`, `tailnetLockKey`,
  `tailnetLockError`, `sshEnabled`, `distro`, `postureIdentity` and
  `clientConnectivity` (endpoints, `mappingVariesByDestIP`, per-DERP-region
  `latency` keyed by region **name**, and `clientSupports`). The default field
  set stays exactly Tailscale's.

## Adding an endpoint

Worked example: the keys resource (`keys.go`) = Tailscale auth keys = Slopscale
pre-auth keys.

1. **Read the Tailscale spec.** Find the operation in the [Tailscale API
   reference](https://tailscale.com/api) (OpenAPI 3.1). Note method, path,
   request/response schema, and which variant(s) Slopscale supports (auth keys
   only, for keys).

2. **Capture golden samples.** Pull the request + response JSON examples from the
   spec, prune to the variant, and use them as the assertion in the contract
   test. _Acceptance: the captured request and response are recorded in the
   test._

3. **Map to Slopscale.** Write the field ↔ field ↔ `state` call mapping. Record
   gaps and the decision for each (e.g. Tailscale `preauthorized` has no
   Slopscale equivalent: accepted, ignored, echoed back). _Acceptance: every
   request field is consumed or deliberately ignored; every response field has a
   source._

4. **Implement the Huma operation.** Declare named request/response structs with
   validation/`default`/`example`/`doc` tags; tag the operation `Tailscale
compat`; declare its `Errors`; enforce the tailnet and scope. Map state
   errors with `mapError`. _Acceptance: `go build ./hscontrol/api/v2/` and the
   operation appears in `Spec()`._

5. **Contract test (in-process, `humatest`).** Assert the server accepts the
   golden request and returns the golden response shape, with secrets and
   timestamps neutralised. Pin the wire facts (e.g. `expirySeconds` in seconds,
   the list `{"keys":[...]}` envelope, the error `message`). See
   `hscontrol/apiv2_keys_test.go`. _Acceptance: the test is green._

6. **Roundtrip the real clients.** Add a `t.Run` subtest to `TestAPIv2`
   (`hscontrol/servertest/apiv2_test.go`) for each of the Go client, tscli, and
   OpenTofu, full create→read→list→delete against one shared server on a real
   loopback port (`servertest.WithRealListener`). tscli and tofu come from the
   nix dev shell; a missing binary fails the test. _Acceptance: `nix develop -c
go test ./hscontrol/servertest/ -run TestAPIv2` is green._

7. **Update the CLI** only if the v2 operation fully replaces a v1 one. Tailscale
   has no separate key-expire verb (its `DELETE` _is_ the revoke), so v2 maps
   `DELETE` to a soft revoke: the key stays retrievable with `invalid: true`
   until the collector reaps it (`preauth_keys.revoked_retention`), the
   equivalent of v1 `preauthkeys expire`. `slopscale preauthkeys` still stays on
   v1 for now (it is the cross-user admin surface), but the verb gap that
   previously blocked migration is closed.
