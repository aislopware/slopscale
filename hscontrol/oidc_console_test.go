package hscontrol

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
)

// TestIsConfiguredAdminEmailVerified pins that oidc.admin_users only
// promotes an address the provider vouched for when the configuration
// demands verification: without the check, any provider that lets a person
// set an arbitrary email hands out the admin role.
func TestIsConfiguredAdminEmailVerified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		verifiedRequired bool
		email            string
		verified         bool
		want             bool
	}{
		{
			name:             "verified and listed",
			verifiedRequired: true,
			email:            "admin@example.com",
			verified:         true,
			want:             true,
		},
		{
			name:             "unverified and listed",
			verifiedRequired: true,
			email:            "admin@example.com",
			verified:         false,
			want:             false,
		},
		{
			name:             "unverified but verification not required",
			verifiedRequired: false,
			email:            "admin@example.com",
			verified:         false,
			want:             true,
		},
		{
			name:             "verified but not listed",
			verifiedRequired: true,
			email:            "other@example.com",
			verified:         true,
			want:             false,
		},
		{name: "no email", verifiedRequired: true, email: "", verified: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := &AuthProviderOIDC{cfg: &types.OIDCConfig{
				AdminUsers:            []string{"Admin@Example.com"},
				EmailVerifiedRequired: tt.verifiedRequired,
			}}

			claims := &types.OIDCClaims{
				Email:         tt.email,
				EmailVerified: types.FlexibleBoolean(tt.verified),
			}

			assert.Equal(t, tt.want, a.isConfiguredAdmin(claims))
		})
	}
}
