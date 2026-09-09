package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
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
		var target *bool

		switch types.SettingKey(r.Setting.Key) {
		case types.SettingDevicesApprovalOn:
			target = &settings.DevicesApprovalOn
		case types.SettingUsersApprovalOn:
			target = &settings.UsersApprovalOn
		case types.SettingPostureIdentityOn:
			target = &settings.PostureIdentityOn
		case types.SettingSSHRecorders:
			err := json.Unmarshal([]byte(r.Setting.Value), &settings.SSHRecorders)
			if err != nil {
				return types.Settings{}, fmt.Errorf(
					"%w: %q holds %q",
					ErrSettingNotList,
					r.Setting.Key,
					r.Setting.Value,
				)
			}

			continue
		case types.SettingSSHRecordingEnforce:
			target = &settings.SSHRecordingEnforce
		case types.SettingDeviceAttributesOn:
			target = &settings.DeviceAttributesOn
		case types.SettingKeyExpiry:
			d, err := time.ParseDuration(r.Setting.Value)
			if err != nil {
				return types.Settings{}, fmt.Errorf(
					"%w: %q holds %q", ErrSettingNotDuration, r.Setting.Key, r.Setting.Value,
				)
			}

			settings.KeyExpiry = d

			continue
		case types.SettingDNS, types.SettingDERP, types.SettingIDTokenKey, types.SettingTailnetLock:
			// Hold JSON or key material and are read by LoadDNSSettings,
			// LoadDERPSettings, LoadIDTokenKey and LoadTailnetLock.
			continue
		default:
			continue
		}

		on, err := strconv.ParseBool(r.Setting.Value)
		if err != nil {
			return types.Settings{}, fmt.Errorf("%w: %q holds %q", ErrSettingNotBoolean, r.Setting.Key, r.Setting.Value)
		}

		*target = on
	}

	return settings, nil
}

// LoadDNSSettings reads the DNS override. It returns nil without error when
// no override is stored and the config file is in force.
func (hsdb *HSDatabase) LoadDNSSettings() (*types.DNSSettings, error) {
	var records []settingRecord

	err := hsdb.ex.query(
		jet.SELECT(table.Settings.AllColumns).
			FROM(table.Settings).
			WHERE(table.Settings.Key.EQ(jet.String(string(types.SettingDNS)))),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading dns settings: %w", err)
	}

	if len(records) == 0 {
		return nil, nil //nolint:nilnil // no row means no override, which is not an error
	}

	var settings types.DNSSettings

	err = json.Unmarshal([]byte(records[0].Setting.Value), &settings)
	if err != nil {
		return nil, fmt.Errorf("decoding dns settings: %w", err)
	}

	return &settings, nil
}

// SaveDNSSettings writes the DNS override as JSON, inserting its row on
// first use.
func (hsdb *HSDatabase) SaveDNSSettings(settings types.DNSSettings) error {
	value, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encoding dns settings: %w", err)
	}

	return hsdb.Write(func(tx *Tx) error {
		return saveSettingValue(tx, types.SettingDNS, string(value))
	})
}

// DeleteDNSSettings removes the DNS override so the config file is in
// force again. Deleting a missing row is not an error.
func (hsdb *HSDatabase) DeleteDNSSettings() error {
	return hsdb.Write(func(tx *Tx) error {
		_, err := tx.executor().exec(
			table.Settings.DELETE().WHERE(table.Settings.Key.EQ(jet.String(string(types.SettingDNS)))),
		)
		if err != nil {
			return fmt.Errorf("deleting dns settings: %w", err)
		}

		return nil
	})
}

// LoadDERPSettings reads the DERP override. It returns nil without error
// when no override is stored and the config file is in force.
func (hsdb *HSDatabase) LoadDERPSettings() (*types.DERPSettings, error) {
	var records []settingRecord

	err := hsdb.ex.query(
		jet.SELECT(table.Settings.AllColumns).
			FROM(table.Settings).
			WHERE(table.Settings.Key.EQ(jet.String(string(types.SettingDERP)))),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading derp settings: %w", err)
	}

	if len(records) == 0 {
		return nil, nil //nolint:nilnil // no row means no override, which is not an error
	}

	var settings types.DERPSettings

	err = json.Unmarshal([]byte(records[0].Setting.Value), &settings)
	if err != nil {
		return nil, fmt.Errorf("decoding derp settings: %w", err)
	}

	return &settings, nil
}

// SaveDERPSettings writes the DERP override as JSON, inserting its row on
// first use.
func (hsdb *HSDatabase) SaveDERPSettings(settings types.DERPSettings) error {
	value, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encoding derp settings: %w", err)
	}

	return hsdb.Write(func(tx *Tx) error {
		return saveSettingValue(tx, types.SettingDERP, string(value))
	})
}

// DeleteDERPSettings removes the DERP override so the config file is in
// force again. Deleting a missing row is not an error.
func (hsdb *HSDatabase) DeleteDERPSettings() error {
	return hsdb.Write(func(tx *Tx) error {
		_, err := tx.executor().exec(
			table.Settings.DELETE().WHERE(table.Settings.Key.EQ(jet.String(string(types.SettingDERP)))),
		)
		if err != nil {
			return fmt.Errorf("deleting derp settings: %w", err)
		}

		return nil
	})
}

// SaveSetting writes one tailnet-wide switch, inserting its row on first use.
func (hsdb *HSDatabase) SaveSetting(key types.SettingKey, on bool) error {
	return hsdb.Write(func(tx *Tx) error {
		return SaveSetting(tx, key, on)
	})
}

// SaveSetting writes one tailnet-wide switch, inserting its row on first use.
func SaveSetting(q Querier, key types.SettingKey, on bool) error {
	return saveSettingValue(q, key, strconv.FormatBool(on))
}

// SaveSSHRecording writes the default session recorders and whether they
// are enforced.
func (hsdb *HSDatabase) SaveSSHRecording(recorders []string, enforce bool) error {
	if recorders == nil {
		recorders = []string{}
	}

	list, err := json.Marshal(recorders)
	if err != nil {
		return fmt.Errorf("encoding SSH recorders: %w", err)
	}

	return hsdb.Write(func(tx *Tx) error {
		err := saveSettingValue(tx, types.SettingSSHRecorders, string(list))
		if err != nil {
			return err
		}

		return SaveSetting(tx, types.SettingSSHRecordingEnforce, enforce)
	})
}

// SaveKeyExpiry writes the key expiry cap, inserting its row on first use.
func (hsdb *HSDatabase) SaveKeyExpiry(d time.Duration) error {
	return hsdb.Write(func(tx *Tx) error {
		return saveSettingValue(tx, types.SettingKeyExpiry, d.String())
	})
}

func saveSettingValue(q Querier, key types.SettingKey, value string) error {
	row := settingRow{Key: string(key), Value: value, UpdatedAt: time.Now().UTC()}

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
	// ErrSettingNotDuration is returned when the key expiry row holds
	// something time.ParseDuration cannot read.
	ErrSettingNotDuration = errors.New("setting is not a duration")
	ErrSettingNotList     = errors.New("setting is not a JSON list")
)
