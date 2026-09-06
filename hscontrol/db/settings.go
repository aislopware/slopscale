package db

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

// settingRow is a settings table row: one tailnet-wide switch.
type settingRow struct {
	Key       string `sql:"primary_key"`
	Value     string
	UpdatedAt time.Time
}

type settingRecord struct {
	Setting settingRow `alias:"settings"`
}

// LoadSettings reads every tailnet-wide switch. A switch without a row
// keeps its zero value.
func (hsdb *HSDatabase) LoadSettings() (types.Settings, error) {
	var records []settingRecord

	err := hsdb.ex.query(jet.SELECT(table.Settings.AllColumns).FROM(table.Settings), &records)
	if err != nil {
		return types.Settings{}, fmt.Errorf("loading settings: %w", err)
	}

	var settings types.Settings

	for _, r := range records {
		on, err := strconv.ParseBool(r.Setting.Value)
		if err != nil {
			return types.Settings{}, fmt.Errorf("%w: %q holds %q", ErrSettingNotBoolean, r.Setting.Key, r.Setting.Value)
		}

		switch types.SettingKey(r.Setting.Key) {
		case types.SettingDevicesApprovalOn:
			settings.DevicesApprovalOn = on
		case types.SettingUsersApprovalOn:
			settings.UsersApprovalOn = on
		}
	}

	return settings, nil
}

// SaveSetting writes one tailnet-wide switch, inserting its row on first use.
func (hsdb *HSDatabase) SaveSetting(key types.SettingKey, on bool) error {
	return hsdb.Write(func(tx *Tx) error {
		return SaveSetting(tx, key, on)
	})
}

// SaveSetting writes one tailnet-wide switch, inserting its row on first use.
func SaveSetting(q Querier, key types.SettingKey, on bool) error {
	row := settingRow{Key: string(key), Value: strconv.FormatBool(on), UpdatedAt: time.Now().UTC()}

	updated, err := q.executor().exec(
		table.Settings.UPDATE(table.Settings.Value, table.Settings.UpdatedAt).
			MODEL(&row).
			WHERE(table.Settings.Key.EQ(jet.String(row.Key))),
	)
	if err != nil {
		return fmt.Errorf("updating setting %q: %w", key, err)
	}

	if updated > 0 {
		return nil
	}

	_, err = q.executor().exec(table.Settings.INSERT(table.Settings.AllColumns).MODEL(&row))
	if err != nil {
		return fmt.Errorf("inserting setting %q: %w", key, err)
	}

	return nil
}

var (
	// ErrUnknownSetting is returned for a setting key the server does not know.
	ErrUnknownSetting = errors.New("unknown setting")
	// ErrSettingNotBoolean is returned when a settings row holds something
	// other than true or false.
	ErrSettingNotBoolean = errors.New("setting is not a boolean")
)
