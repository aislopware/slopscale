package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostureIntegrationNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		integration PostureIntegration
		wantErr     error
		check       func(t *testing.T, got PostureIntegration)
	}{
		{
			name: "empty name",
			integration: PostureIntegration{
				Name:     "",
				Provider: PostureProviderFalcon,
			},
			wantErr: ErrPostureIntegrationName,
		},
		{
			name: "whitespace name",
			integration: PostureIntegration{
				Name:     "   ",
				Provider: PostureProviderFalcon,
			},
			wantErr: ErrPostureIntegrationName,
		},
		{
			name: "name exceeds 63 characters",
			integration: PostureIntegration{
				Name:     strings.Repeat("a", 64),
				Provider: PostureProviderFalcon,
			},
			wantErr: ErrPostureIntegrationName,
		},
		{
			name: "name exactly 63 characters is valid",
			integration: PostureIntegration{
				Name:     strings.Repeat("a", 63),
				Provider: PostureProviderFalcon,
				Config: PostureIntegrationConfig{
					ClientID:     "cid",
					ClientSecret: "csec",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, strings.Repeat("a", 63), got.Name)
			},
		},
		{
			name: "name is trimmed",
			integration: PostureIntegration{
				Name:     "  falcon-integration  ",
				Provider: PostureProviderFalcon,
				Config: PostureIntegrationConfig{
					ClientID:     "cid",
					ClientSecret: "csec",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, "falcon-integration", got.Name)
			},
		},
		{
			name: "unknown provider",
			integration: PostureIntegration{
				Name:     "unknown",
				Provider: "invalid-provider",
			},
			wantErr: ErrPostureIntegrationProvider,
		},
		{
			name: "invalid base url scheme",
			integration: PostureIntegration{
				Name:     "bad-url",
				Provider: PostureProviderKolide,
				Config: PostureIntegrationConfig{
					APIToken: "token",
					BaseURL:  "ftp://example.com",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "invalid base url missing host",
			integration: PostureIntegration{
				Name:     "bad-url",
				Provider: PostureProviderKolide,
				Config: PostureIntegrationConfig{
					APIToken: "token",
					BaseURL:  "https://",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "base url trailing slash stripped",
			integration: PostureIntegration{
				Name:     "slash-strip",
				Provider: PostureProviderKolide,
				Config: PostureIntegrationConfig{
					APIToken: "token",
					BaseURL:  "https://api.kolide.com/",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, "https://api.kolide.com", got.Config.BaseURL)
			},
		},
		{
			name: "falcon missing client id",
			integration: PostureIntegration{
				Name:     "falcon",
				Provider: PostureProviderFalcon,
				Config: PostureIntegrationConfig{
					ClientSecret: "secret",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "falcon missing client secret",
			integration: PostureIntegration{
				Name:     "falcon",
				Provider: PostureProviderFalcon,
				Config: PostureIntegrationConfig{
					ClientID: "id",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "falcon default base url applied when empty",
			integration: PostureIntegration{
				Name:     "falcon",
				Provider: PostureProviderFalcon,
				Config: PostureIntegrationConfig{
					ClientID:     "id",
					ClientSecret: "secret",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, "https://api.crowdstrike.com", got.Config.BaseURL)
			},
		},
		{
			name: "falcon custom base url preserved",
			integration: PostureIntegration{
				Name:     "falcon",
				Provider: PostureProviderFalcon,
				Config: PostureIntegrationConfig{
					ClientID:     "id",
					ClientSecret: "secret",
					BaseURL:      "https://api.us-2.crowdstrike.com",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, "https://api.us-2.crowdstrike.com", got.Config.BaseURL)
			},
		},
		{
			name: "jamf missing client id",
			integration: PostureIntegration{
				Name:     "jamf",
				Provider: PostureProviderJamf,
				Config: PostureIntegrationConfig{
					ClientSecret: "secret",
					BaseURL:      "https://jamf.example.com",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "jamf missing client secret",
			integration: PostureIntegration{
				Name:     "jamf",
				Provider: PostureProviderJamf,
				Config: PostureIntegrationConfig{
					ClientID: "id",
					BaseURL:  "https://jamf.example.com",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "jamf missing base url",
			integration: PostureIntegration{
				Name:     "jamf",
				Provider: PostureProviderJamf,
				Config: PostureIntegrationConfig{
					ClientID:     "id",
					ClientSecret: "secret",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "jamf valid",
			integration: PostureIntegration{
				Name:     "jamf",
				Provider: PostureProviderJamf,
				Config: PostureIntegrationConfig{
					ClientID:     "id",
					ClientSecret: "secret",
					BaseURL:      "https://jamf.example.com",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, "https://jamf.example.com", got.Config.BaseURL)
			},
		},
		{
			name: "intune missing client id",
			integration: PostureIntegration{
				Name:     "intune",
				Provider: PostureProviderIntune,
				Config: PostureIntegrationConfig{
					ClientSecret: "secret",
					TenantID:     "tenant",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "intune missing client secret",
			integration: PostureIntegration{
				Name:     "intune",
				Provider: PostureProviderIntune,
				Config: PostureIntegrationConfig{
					ClientID: "id",
					TenantID: "tenant",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "intune missing tenant id",
			integration: PostureIntegration{
				Name:     "intune",
				Provider: PostureProviderIntune,
				Config: PostureIntegrationConfig{
					ClientID:     "id",
					ClientSecret: "secret",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "intune valid without base url",
			integration: PostureIntegration{
				Name:     "intune",
				Provider: PostureProviderIntune,
				Config: PostureIntegrationConfig{
					ClientID:     "id",
					ClientSecret: "secret",
					TenantID:     "tenant",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Empty(t, got.Config.BaseURL)
			},
		},
		{
			name: "sentinelone missing api token",
			integration: PostureIntegration{
				Name:     "s1",
				Provider: PostureProviderSentinelOne,
				Config: PostureIntegrationConfig{
					BaseURL: "https://usea1.sentinelone.net",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "sentinelone missing base url",
			integration: PostureIntegration{
				Name:     "s1",
				Provider: PostureProviderSentinelOne,
				Config: PostureIntegrationConfig{
					APIToken: "token",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "sentinelone valid",
			integration: PostureIntegration{
				Name:     "s1",
				Provider: PostureProviderSentinelOne,
				Config: PostureIntegrationConfig{
					APIToken: "token",
					BaseURL:  "https://usea1.sentinelone.net",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, "https://usea1.sentinelone.net", got.Config.BaseURL)
				assert.Equal(t, "token", got.Config.APIToken)
			},
		},
		{
			name: "kandji missing api token",
			integration: PostureIntegration{
				Name:     "kandji",
				Provider: PostureProviderKandji,
				Config: PostureIntegrationConfig{
					BaseURL: "https://tenant.api.kandji.io",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "kandji missing base url",
			integration: PostureIntegration{
				Name:     "kandji",
				Provider: PostureProviderKandji,
				Config: PostureIntegrationConfig{
					APIToken: "token",
				},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "kandji valid",
			integration: PostureIntegration{
				Name:     "kandji",
				Provider: PostureProviderKandji,
				Config: PostureIntegrationConfig{
					APIToken: "token",
					BaseURL:  "https://tenant.api.kandji.io",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Equal(t, "https://tenant.api.kandji.io", got.Config.BaseURL)
			},
		},
		{
			name: "kolide missing api token",
			integration: PostureIntegration{
				Name:     "kolide",
				Provider: PostureProviderKolide,
				Config:   PostureIntegrationConfig{},
			},
			wantErr: ErrPostureIntegrationConfig,
		},
		{
			name: "kolide valid without base url",
			integration: PostureIntegration{
				Name:     "kolide",
				Provider: PostureProviderKolide,
				Config: PostureIntegrationConfig{
					APIToken: "token",
				},
			},
			wantErr: nil,
			check: func(t *testing.T, got PostureIntegration) {
				assert.Empty(t, got.Config.BaseURL)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			i := tt.integration

			err := i.Normalize()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)

			if tt.check != nil {
				tt.check(t, i)
			}
		})
	}
}

func TestPostureProvider(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "falcon:", PostureProviderFalcon.Prefix())
	assert.Equal(t, "sentinelOne:", PostureProviderSentinelOne.Prefix())
	assert.Equal(t, "intune:", PostureProviderIntune.Prefix())
	assert.Equal(t, "jamfPro:", PostureProviderJamf.Prefix())
	assert.Equal(t, "kandji:", PostureProviderKandji.Prefix())
	assert.Equal(t, "kolide:", PostureProviderKolide.Prefix())
	assert.Empty(t, PostureProvider("unknown").Prefix())

	assert.True(t, PostureProviderFalcon.Valid())
	assert.True(t, PostureProviderSentinelOne.Valid())
	assert.True(t, PostureProviderIntune.Valid())
	assert.True(t, PostureProviderJamf.Valid())
	assert.True(t, PostureProviderKandji.Valid())
	assert.True(t, PostureProviderKolide.Valid())
	assert.False(t, PostureProvider("unknown").Valid())

	prefixes := PostureAttributePrefixes()
	assert.Equal(t, []string{"falcon", "sentinelOne", "intune", "jamfPro", "kandji", "kolide"}, prefixes)
}

func TestPostureIntegrationConfigHasSecret(t *testing.T) {
	t.Parallel()

	assert.False(t, PostureIntegrationConfig{}.HasSecret())
	assert.True(t, PostureIntegrationConfig{ClientSecret: "sec"}.HasSecret())
	assert.True(t, PostureIntegrationConfig{APIToken: "tok"}.HasSecret())
}

func TestPostureIntegrationID(t *testing.T) {
	t.Parallel()

	id := PostureIntegrationID(42)
	assert.Equal(t, "42", id.String())
	assert.Equal(t, uint64(42), id.Uint64())
}
