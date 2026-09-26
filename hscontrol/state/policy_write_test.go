package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	allowAllPolicy = `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`
	denyAllPolicy  = `{"acls":[]}`
)

// newAllowAllState returns a State whose stored and live policy is allow-all.
func newAllowAllState(t *testing.T) *State {
	t.Helper()

	_, s, _ := persistTestSetup(t)

	_, _, err := s.ReplacePolicy(allowAllPolicy)
	require.NoError(t, err)

	rules, _ := s.Filter()
	require.Len(t, rules, 1, "allow-all is in force")

	return s
}

func TestReplacePolicyRefusesAnInvalidPolicy(t *testing.T) {
	t.Parallel()

	s := newAllowAllState(t)

	_, cs, err := s.ReplacePolicy(`{"acls":[{"action":"deny","src":["*"],"dst":["*:*"]}]}`)
	require.ErrorIs(t, err, ErrPolicyRejected)
	assert.Empty(t, cs, "the live policy never changed, so there is nothing to undo")

	rules, _ := s.Filter()
	assert.Len(t, rules, 1, "allow-all stays in force")
}

func TestReplacePolicyRestoresTheStoredPolicyWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	s := newAllowAllState(t)

	for _, stmt := range []string{
		`CREATE TRIGGER refuse_policy_insert BEFORE INSERT ON policies BEGIN SELECT RAISE(ABORT, 'refused'); END`,
		`CREATE TRIGGER refuse_policy_update BEFORE UPDATE ON policies BEGIN SELECT RAISE(ABORT, 'refused'); END`,
	} {
		_, err := s.db.DB.ExecContext(t.Context(), stmt)
		require.NoError(t, err)
	}

	_, cs, err := s.ReplacePolicy(denyAllPolicy)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrPolicyRejected, "a storage failure is the server's fault, not the policy's")
	assert.NotEmpty(t, cs, "clients that saw the refused policy must be moved back")

	rules, _ := s.Filter()
	assert.Len(t, rules, 1, "the stored allow-all is back in force")

	stored, err := s.db.GetPolicy()
	require.NoError(t, err)
	assert.JSONEq(t, allowAllPolicy, stored.Data)
}
