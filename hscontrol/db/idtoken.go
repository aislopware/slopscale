package db

import (
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// LoadIDTokenKey reads the identity token signing key; empty when none
// has been made yet.
func (hsdb *HSDatabase) LoadIDTokenKey() (string, error) {
	return loadIDTokenKey(hsdb.executor())
}

func loadIDTokenKey(ex *executor) (string, error) {
	var records []settingRecord

	err := ex.query(
		jet.SELECT(table.Settings.AllColumns).
			FROM(table.Settings).
			WHERE(table.Settings.Key.EQ(jet.String(string(types.SettingIDTokenKey)))),
		&records,
	)
	if err != nil {
		return "", fmt.Errorf("loading the identity token key: %w", err)
	}

	if len(records) == 0 {
		return "", nil
	}

	return records[0].Setting.Value, nil
}

// EnsureIDTokenKey stores encoded as the identity token signing key
// unless one exists, and returns the key in force: two servers on one
// database that both start signing keep the one that got there first.
func (hsdb *HSDatabase) EnsureIDTokenKey(encoded string) (string, error) {
	stored, err := Write(hsdb, func(tx *Tx) (string, error) {
		existing, err := loadIDTokenKey(tx.executor())
		if err != nil || existing != "" {
			return existing, err
		}

		row := settingRow{Key: string(types.SettingIDTokenKey), Value: encoded, UpdatedAt: time.Now().UTC()}

		_, err = tx.executor().exec(table.Settings.INSERT(table.Settings.AllColumns).MODEL(&row))
		if err != nil {
			return "", fmt.Errorf("storing the identity token key: %w", err)
		}

		return encoded, nil
	})
	if err != nil {
		// The insert lost a race with another server; its key is the one
		// to use.
		existing, loadErr := hsdb.LoadIDTokenKey()
		if loadErr == nil && existing != "" {
			return existing, nil
		}

		return "", err
	}

	return stored, nil
}
