package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDBCommandWiring pins the subcommands and flags the backup
// documentation tells operators to type.
func TestDBCommandWiring(t *testing.T) {
	backup, _, err := rootCmd.Find([]string{"db", "backup"})
	require.NoError(t, err)
	assert.Equal(t, "backup", backup.Name())
	assert.NotNil(t, backup.Flags().Lookup("out"))

	verify, _, err := rootCmd.Find([]string{"db", "verify"})
	require.NoError(t, err)
	assert.Equal(t, "verify", verify.Name())

	restore, _, err := rootCmd.Find([]string{"db", "restore"})
	require.NoError(t, err)
	assert.Equal(t, "restore", restore.Name())
	assert.NotNil(t, restore.Flags().Lookup("yes"))
}
