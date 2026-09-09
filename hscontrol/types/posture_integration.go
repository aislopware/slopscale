package types

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A posture integration asks a device management or endpoint security
// service what it knows about each machine, matched by serial number,
// and turns the answer into posture attributes the policy can check,
// the way Tailscale's device posture integrations do. The attributes
// carry the provider's prefix (falcon:ztaScore, intune:complianceState)
// and the names Tailscale documents, so a posture written for Tailscale
// works unchanged. See docs/ref/device-trust.md.

// PostureIntegrationID identifies an integration row.
type PostureIntegrationID uint64

// String renders the ID in base 10.
func (id PostureIntegrationID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Uint64 returns the ID as a plain integer.
func (id PostureIntegrationID) Uint64() uint64 { return uint64(id) }

// PostureProvider names a supported service.
type PostureProvider string

// The providers, with the attribute prefix each one writes.
const (
	PostureProviderFalcon      PostureProvider = "falcon"
	PostureProviderSentinelOne PostureProvider = "sentinelone"
	PostureProviderIntune      PostureProvider = "intune"
	PostureProviderJamf        PostureProvider = "jamf"
	PostureProviderKandji      PostureProvider = "kandji"
	PostureProviderKolide      PostureProvider = "kolide"
)

// PostureProviders lists every provider in display order.
var PostureProviders = []PostureProvider{
	PostureProviderFalcon, PostureProviderSentinelOne, PostureProviderIntune,
	PostureProviderJamf, PostureProviderKandji, PostureProviderKolide,
}

// Prefix is the attribute prefix the provider writes, Tailscale's.
func (p PostureProvider) Prefix() string {
	switch p {
	case PostureProviderFalcon:
		return "falcon:"
	case PostureProviderSentinelOne:
		return "sentinelOne:"
	case PostureProviderIntune:
		return "intune:"
	case PostureProviderJamf:
		return "jamfPro:"
	case PostureProviderKandji:
		return "kandji:"
	case PostureProviderKolide:
		return "kolide:"
	}

	return ""
}

// Valid reports whether the provider is known.
func (p PostureProvider) Valid() bool {
	return slices.Contains(PostureProviders, p)
}

// PostureAttributePrefixes are the attribute prefixes an integration may
// write, without the colon, for the expression parser and the console.
func PostureAttributePrefixes() []string {
	out := make([]string, 0, len(PostureProviders))
	for _, p := range PostureProviders {
		out = append(out, strings.TrimSuffix(p.Prefix(), ":"))
	}

	return out
}

// PostureIntegrationConfig is how the provider is reached. Which fields
// matter depends on the provider; the rest stay empty.
type PostureIntegrationConfig struct {
	// BaseURL is the API origin: the Falcon cloud, the SentinelOne
	// console, the Jamf Pro server, the Kandji tenant. Intune and Kolide
	// have fixed endpoints, so it stays empty for them unless a test
	// server stands in.
	BaseURL string `json:"baseUrl,omitempty"`
	// ClientID and ClientSecret are an OAuth client (Falcon, Intune,
	// Jamf Pro).
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	// APIToken is a bearer or API token (SentinelOne, Kandji, Kolide).
	APIToken string `json:"apiToken,omitempty"`
	// TenantID is the Entra tenant (Intune).
	TenantID string `json:"tenantId,omitempty"`
}

// HasSecret reports whether a credential is stored.
func (c PostureIntegrationConfig) HasSecret() bool {
	return c.ClientSecret != "" || c.APIToken != ""
}

// PostureIntegration is one configured provider.
type PostureIntegration struct {
	ID       PostureIntegrationID
	Provider PostureProvider
	Name     string
	Config   PostureIntegrationConfig
	Enabled  bool

	// LastSyncAt is when the provider was last asked, LastError what
	// went wrong then (empty after a good sync) and LastMatched how many
	// machines the provider knew.
	LastSyncAt  time.Time
	LastError   string
	LastMatched int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Errors an integration definition can fail with.
var (
	ErrPostureIntegrationNotFound  = errors.New("posture integration not found")
	ErrPostureIntegrationProvider  = errors.New("unknown posture provider")
	ErrPostureIntegrationName      = errors.New("posture integration name must be 1 to 63 characters")
	ErrPostureIntegrationNameTaken = errors.New("a posture integration with that name exists")
	ErrPostureIntegrationConfig    = errors.New("invalid posture integration configuration")
)

const maxPostureIntegrationNameLength = 63

// Normalize trims the fields and checks that the provider has what it
// needs. Secrets left empty on an update keep their stored value; that
// merge is the caller's, before Normalize runs.
func (i *PostureIntegration) Normalize() error {
	i.Name = strings.TrimSpace(i.Name)
	if i.Name == "" || len(i.Name) > maxPostureIntegrationNameLength {
		return ErrPostureIntegrationName
	}

	if !i.Provider.Valid() {
		return fmt.Errorf("%w: %q", ErrPostureIntegrationProvider, i.Provider)
	}

	c := &i.Config
	c.BaseURL = strings.TrimSuffix(strings.TrimSpace(c.BaseURL), "/")
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.ClientSecret = strings.TrimSpace(c.ClientSecret)
	c.APIToken = strings.TrimSpace(c.APIToken)
	c.TenantID = strings.TrimSpace(c.TenantID)

	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%w: base URL must be an http(s) origin", ErrPostureIntegrationConfig)
		}
	}

	switch i.Provider {
	case PostureProviderFalcon, PostureProviderJamf:
		if c.ClientID == "" || c.ClientSecret == "" {
			return fmt.Errorf("%w: %s needs a client id and secret", ErrPostureIntegrationConfig, i.Provider)
		}

		if c.BaseURL == "" {
			if i.Provider == PostureProviderJamf {
				return fmt.Errorf("%w: jamf needs the Jamf Pro server URL", ErrPostureIntegrationConfig)
			}

			c.BaseURL = "https://api.crowdstrike.com"
		}
	case PostureProviderIntune:
		if c.ClientID == "" || c.ClientSecret == "" || c.TenantID == "" {
			return fmt.Errorf("%w: intune needs a tenant id, client id and secret", ErrPostureIntegrationConfig)
		}
	case PostureProviderSentinelOne, PostureProviderKandji:
		if c.APIToken == "" || c.BaseURL == "" {
			return fmt.Errorf("%w: %s needs the console URL and an API token", ErrPostureIntegrationConfig, i.Provider)
		}
	case PostureProviderKolide:
		if c.APIToken == "" {
			return fmt.Errorf("%w: kolide needs an API key", ErrPostureIntegrationConfig)
		}
	}

	return nil
}
