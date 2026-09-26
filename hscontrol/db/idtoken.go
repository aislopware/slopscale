package db

import (
	"github.com/aislopware/slopscale/hscontrol/types"
)

// LoadIDTokenKey reads the identity token signing key; empty when none
// has been made yet.
func (hsdb *HSDatabase) LoadIDTokenKey() (string, error) {
	return loadSettingValue(hsdb.executor(), types.SettingIDTokenKey)
}

// EnsureIDTokenKey stores encoded as the identity token signing key
// unless one exists, and returns the key in force: two servers on one
// database that both start signing keep the one that got there first.
func (hsdb *HSDatabase) EnsureIDTokenKey(encoded string) (string, error) {
	return hsdb.ensureSettingValue(types.SettingIDTokenKey, encoded)
}

// LoadTailnetID reads the tailnet's stable ID; empty when none has been
// made yet.
func (hsdb *HSDatabase) LoadTailnetID() (string, error) {
	return loadSettingValue(hsdb.executor(), types.SettingTailnetID)
}

// EnsureTailnetID stores id as the tailnet's stable ID unless one exists,
// and returns the ID in force, so servers sharing a database agree on it.
func (hsdb *HSDatabase) EnsureTailnetID(id string) (string, error) {
	return hsdb.ensureSettingValue(types.SettingTailnetID, id)
}
