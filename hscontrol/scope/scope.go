// Package scope models the OAuth capability scopes the Headscale v2 API enforces
// and the rule for whether a granted set of scopes satisfies a required one.
//
// The vocabulary is taken from Tailscale's OpenAPI spec (the same scope names
// the Terraform provider and Kubernetes operator request), so a client written
// against Tailscale's scopes works unchanged against Headscale. The grant
// predicate is kept here, separate from the HTTP/huma layer in
// hscontrol/api/v2, so it can be tested exhaustively on its own.
package scope

import (
	"strings"

	"github.com/juanfont/headscale/hscontrol/types"
)

// Scope is an OAuth capability an operation requires and a token grants. The names
// mirror Tailscale's API scopes; a "...:read" scope is the read-only subset of its
// write scope.
type Scope string

const (
	// All and AllRead are Tailscale's forward-compatible super-scopes: "all"
	// grants every other scope, "all:read" grants every :read subset.
	All     Scope = "all"
	AllRead Scope = "all:read"

	AuthKeys     Scope = "auth_keys"
	AuthKeysRead Scope = "auth_keys:read"

	// OAuthKeys gates managing OAuth clients (keyType:"client" on the keys
	// resource).
	OAuthKeys     Scope = "oauth_keys"
	OAuthKeysRead Scope = "oauth_keys:read"

	DevicesCore     Scope = "devices:core"
	DevicesCoreRead Scope = "devices:core:read"

	DevicesRoutes     Scope = "devices:routes"
	DevicesRoutesRead Scope = "devices:routes:read"

	PolicyFile     Scope = "policy_file"
	PolicyFileRead Scope = "policy_file:read"

	FeatureSettings     Scope = "feature_settings"
	FeatureSettingsRead Scope = "feature_settings:read"

	Users     Scope = "users"
	UsersRead Scope = "users:read"

	// DNS gates the tailnet's DNS settings: nameservers, split DNS,
	// search domains and extra records.
	DNS     Scope = "dns"
	DNSRead Scope = "dns:read"

	// Webhooks gates the webhook endpoints, which Tailscale lets every
	// admin role manage.
	Webhooks     Scope = "webhooks"
	WebhooksRead Scope = "webhooks:read"

	// LogsConfigurationRead gates the audit log, which Tailscale calls the
	// configuration log. There is no write scope: the log is append-only
	// and written by the server.
	LogsConfigurationRead Scope = "logs:configuration:read"
)

const readSuffix = ":read"

// Known returns every scope in the vocabulary, in a stable order. Useful for
// exhaustive iteration in tests and documentation.
func Known() []Scope {
	return []Scope{
		All, AllRead,
		AuthKeys, AuthKeysRead,
		OAuthKeys, OAuthKeysRead,
		DevicesCore, DevicesCoreRead,
		DevicesRoutes, DevicesRoutesRead,
		PolicyFile, PolicyFileRead,
		FeatureSettings, FeatureSettingsRead,
		Users, UsersRead,
		DNS, DNSRead,
		Webhooks, WebhooksRead,
		LogsConfigurationRead,
	}
}

// IsRead reports whether s is a read-only scope (its name ends with ":read").
func (s Scope) IsRead() bool {
	return strings.HasSuffix(string(s), readSuffix)
}

// IsWrite reports whether s is a non-empty write scope.
func (s Scope) IsWrite() bool {
	return s != "" && !s.IsRead()
}

// Parse converts scope strings (as stored on a token or client) into Scope values.
// Unknown strings are kept as-is; they simply never satisfy any required scope.
func Parse(ss []string) []Scope {
	out := make([]Scope, len(ss))
	for i, s := range ss {
		out[i] = Scope(s)
	}

	return out
}

// Grants reports whether the granted scopes satisfy the required want scope.
func Grants(granted []Scope, want Scope) bool {
	for _, g := range granted {
		if satisfies(g, want) {
			return true
		}
	}

	return false
}

// satisfies reports whether a single held scope satisfies want: exact match; a
// write scope grants its own :read subset; "all" grants everything; "all:read"
// grants any :read scope.
func satisfies(have, want Scope) bool {
	if have == want || have == All {
		return true
	}

	if have == AllRead {
		return want.IsRead()
	}

	// A write scope grants its own read subset, e.g. auth_keys ⊇ auth_keys:read.
	return string(want) == string(have)+readSuffix
}

// RequiresTags reports whether any scope obliges a credential to carry tags:
// devices:core and auth_keys mint tagged, tailnet-owned credentials.
func RequiresTags(scopes []Scope) bool {
	for _, s := range scopes {
		if s == DevicesCore || s == AuthKeys {
			return true
		}
	}

	return false
}

// ForRole returns the scopes a user role holds. A credential that acts as a
// user (an API key with an owner, an OAuth client created by one) can never
// do more than this, whatever scopes it was minted with. The table follows
// Tailscale's role matrix: owner and admin do everything; a network admin
// manages the policy, routes and DNS and reads the rest; an IT admin manages
// users, devices and keys and reads the policy; both manage webhooks and
// read the audit log;
// an auditor reads everything; a member has no admin access.
func ForRole(role types.Role) []Scope {
	switch role {
	case types.RoleOwner, types.RoleAdmin:
		return []Scope{All}
	case types.RoleNetworkAdmin:
		return []Scope{
			PolicyFile, DevicesRoutes, DNS, Webhooks,
			UsersRead, DevicesCoreRead, AuthKeysRead, OAuthKeysRead, FeatureSettingsRead,
			LogsConfigurationRead,
		}
	case types.RoleITAdmin:
		return []Scope{
			Users, DevicesCore, AuthKeys, OAuthKeys, FeatureSettings, Webhooks,
			PolicyFileRead, DevicesRoutesRead, DNSRead,
			LogsConfigurationRead,
		}
	case types.RoleAuditor:
		return []Scope{AllRead}
	case types.RoleMember:
		return nil
	}

	return nil
}

// Narrow returns the subset of wanted that granted allows, in wanted's order.
// It bounds a credential minted by another (an OAuth client created through
// a role-limited API key) to the creator's authority.
func Narrow(granted, wanted []Scope) []Scope {
	var out []Scope

	for _, w := range wanted {
		if Grants(granted, w) {
			out = append(out, w)
		}
	}

	return out
}
