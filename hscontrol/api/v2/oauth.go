package apiv2

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

// grantAuditAction is the audit action of a client-credentials exchange.
// The endpoint is a plain route, outside the middleware that audits every
// huma operation, so it records its own entry for both outcomes.
const grantAuditAction = "oauth.token.create"

// accessTokenTTL is the fixed lifetime of a minted access token, matching
// Tailscale's non-configurable one hour.
const accessTokenTTL = time.Hour

// registerOAuthToken mounts POST /api/v2/oauth/token on the router. It is a plain
// handler, not a Huma operation: it consumes application/x-www-form-urlencoded
// and emits RFC 6749 OAuth2 error bodies ({"error","error_description"}), neither
// of which fits Huma's JSON-in / Tailscale-error-out machinery.
// ponytail: one bespoke OAuth endpoint isn't worth bending Huma around.
func registerOAuthToken(router chi.Router, b Backend) {
	router.Post("/api/v2/oauth/token", oauthTokenHandler(b))
}

// tokenResponse is the OAuth 2.0 client-credentials success body.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

// oauthTokenHandler implements the client-credentials grant (RFC 6749 §4.4):
// authenticate the client, optionally narrow the granted scopes/tags, and mint a
// short-lived bearer token.
func oauthTokenHandler(b Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// deny answers a refused request and records it, with whatever
		// client the presented secret named.
		deny := func(secret string, status int, code, desc string) {
			writeOAuthError(w, status, code, desc)
			recordTokenEvent(b, r, clientIDFromSecret(secret), status,
				map[string]any{"error": code})
		}

		err := r.ParseForm()
		if err != nil {
			deny("", http.StatusBadRequest, "invalid_request", "could not parse request body")

			return
		}

		// grant_type defaults to client_credentials: Tailscale's documented curl
		// omits it, and the x/oauth2 client always sends it.
		if gt := r.PostForm.Get("grant_type"); gt != "" && gt != "client_credentials" {
			deny("", http.StatusBadRequest, "unsupported_grant_type",
				"only the client_credentials grant is supported")

			return
		}

		// Credentials may arrive in the body or as HTTP Basic (the x/oauth2
		// auto-detect probes Basic first). The secret embeds the client id, so a
		// separate client_id is not required.
		secret := r.PostForm.Get("client_secret")
		if secret == "" {
			if _, pass, ok := r.BasicAuth(); ok {
				secret = pass
			}
		}

		if secret == "" {
			deny("", http.StatusUnauthorized, "invalid_client", "missing client credentials")

			return
		}

		client, err := b.State.AuthenticateOAuthClient(secret)
		if err != nil {
			deny(secret, http.StatusUnauthorized, "invalid_client", "invalid client credentials")

			return
		}

		// Optional space-delimited scope/tags narrow the token to a subset of the
		// client's grant.
		scopes, badScope, ok := narrowScopes(client.Scopes, strings.Fields(r.PostForm.Get("scope")))
		if !ok {
			deny(secret, http.StatusBadRequest, "invalid_scope",
				"scope "+badScope+" is not granted to this client")

			return
		}

		tags, badTag, ok := narrowTags(client, strings.Fields(r.PostForm.Get("tags")))
		if !ok {
			deny(secret, http.StatusBadRequest, "invalid_target",
				"tag "+badTag+" is not granted to this client")

			return
		}

		expiry := time.Now().Add(accessTokenTTL)

		tokenStr, _, err := b.State.MintAccessToken(client.ClientID, scopes, tags, &expiry)
		if err != nil {
			deny(secret, http.StatusInternalServerError, "server_error", "could not mint access token")

			return
		}

		recordTokenEvent(b, r, client.ClientID, http.StatusOK,
			map[string]any{"scopes": scopes, "tags": tags})

		writeJSON(w, http.StatusOK, tokenResponse{
			AccessToken: tokenStr,
			TokenType:   "Bearer",
			ExpiresIn:   int(accessTokenTTL.Seconds()),
			Scope:       strings.Join(scopes, " "),
		})
	}
}

// recordTokenEvent writes the audit entry for one token exchange. A refused
// attempt is recorded too: the endpoint is unauthenticated, so a run of
// failures against one client id is what a credential hunt looks like.
func recordTokenEvent(b Backend, r *http.Request, clientID string, outcome int, detail map[string]any) {
	audit.Record(b.State, &types.AuditEvent{
		Action:     grantAuditAction,
		ActorKind:  types.ActorOAuth,
		ActorName:  clientID,
		TargetKind: "oauth_client",
		TargetID:   clientID,
		Outcome:    outcome,
		Detail:     detail,
		RemoteAddr: r.RemoteAddr,
	})
}

// clientIDFromSecret recovers the public client id a secret carries
// (hskey-client-<id>-<secret>), so a refused attempt names the client it
// aimed at. It returns "" for anything not shaped like a client secret.
func clientIDFromSecret(secret string) string {
	secret, _, _ = strings.Cut(secret, "?")

	rest, found := strings.CutPrefix(secret, types.OAuthClientPrefix)
	if !found {
		return ""
	}

	id, _, found := strings.Cut(rest, "-")
	if !found {
		return ""
	}

	return id
}

// narrowScopes returns the requested scopes if each is granted by the client (an
// empty request means "the client's full grant"), otherwise the offending scope
// and false. A client holding a broad scope (e.g. "all") may mint a token limited
// to a narrower one.
func narrowScopes(granted, requested []string) ([]string, string, bool) {
	if len(requested) == 0 {
		return granted, "", true
	}

	for _, req := range requested {
		if !scope.Grants(scope.Parse(granted), scope.Scope(req)) {
			return nil, req, false
		}
	}

	return requested, "", true
}

// narrowTags returns the requested tags if each is within the client's grant (an
// empty request means "the client's full tag set"), otherwise the offending tag
// and false. A client with the "all" scope may assign any tag, matching Tailscale.
func narrowTags(client *types.OAuthClient, requested []string) ([]string, string, bool) {
	if len(requested) == 0 {
		return client.Tags, "", true
	}

	allScope := slices.Contains(client.Scopes, string(scope.All))

	for _, req := range requested {
		// Reject malformed tags at the trust boundary. A client's own tags are
		// validated at creation, so this only matters for an "all"-scope client,
		// whose tags would otherwise skip the membership check below and flow
		// unvalidated into auth-key creation.
		if !strings.HasPrefix(req, "tag:") {
			return nil, req, false
		}

		if !allScope && !slices.Contains(client.Tags, req) {
			return nil, req, false
		}
	}

	return requested, "", true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	// Token responses carry bearer credentials; RFC 6749 §5.1 forbids caching
	// them. writeJSON serves only the token endpoint, so set it unconditionally.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) //nolint:errchkjson // best-effort response write of a known-safe value
}

// writeOAuthError emits an RFC 6749 §5.2 error body, which the x/oauth2 client
// parses into its RetrieveError.
func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": desc,
	})
}
