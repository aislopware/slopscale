package scope

import (
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
)

// TestForRoleMatrix pins the role matrix: what each role may write and
// read. A row here is a promise to operators in docs/ref/roles.md.
func TestForRoleMatrix(t *testing.T) {
	t.Parallel()

	writes := []Scope{AuthKeys, OAuthKeys, DevicesCore, DevicesRoutes, PolicyFile, FeatureSettings, Users}

	tests := []struct {
		role       types.Role
		wantWrites []Scope
		readsAll   bool
	}{
		{types.RoleOwner, writes, true},
		{types.RoleAdmin, writes, true},
		{types.RoleNetworkAdmin, []Scope{PolicyFile, DevicesRoutes, Webhooks}, true},
		{types.RoleITAdmin, []Scope{Users, DevicesCore, AuthKeys, OAuthKeys, FeatureSettings, Webhooks}, true},
		{types.RoleAuditor, nil, true},
		{types.RoleMember, nil, false},
		{types.Role(""), nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.role.String(), func(t *testing.T) {
			t.Parallel()

			granted := ForRole(tt.role)

			for _, w := range writes {
				want := false

				for _, allowed := range tt.wantWrites {
					if allowed == w {
						want = true
					}
				}

				assert.Equal(t, want, Grants(granted, w), "write %s", w)
				assert.Equal(t, tt.readsAll || want, Grants(granted, w+readSuffix), "read %s", w)
			}
		})
	}
}

func TestNarrow(t *testing.T) {
	t.Parallel()

	granted := ForRole(types.RoleNetworkAdmin)

	got := Narrow(granted, []Scope{Users, PolicyFile, AuthKeysRead, DevicesRoutes, All})
	assert.Equal(t, []Scope{PolicyFile, AuthKeysRead, DevicesRoutes}, got)

	assert.Nil(t, Narrow(nil, []Scope{Users}), "a member delegates nothing")
	assert.Equal(t, []Scope{All}, Narrow([]Scope{All}, []Scope{All}))
}
