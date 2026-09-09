package db

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// tkaAUMRow is a row of the tka_aums table.
type tkaAUMRow struct {
	Hash        string `sql:"primary_key"`
	PrevHash    *string
	Aum         string
	CommittedAt *time.Time
}

type tkaAUMRecord struct {
	AUM tkaAUMRow `alias:"tka_aums"`
}

// LoadTailnetLock reads the tailnet lock settings; the zero value when
// the lock was never enabled.
func (hsdb *HSDatabase) LoadTailnetLock() (types.TailnetLockSettings, error) {
	var records []settingRecord

	err := hsdb.ex.query(
		jet.SELECT(table.Settings.AllColumns).
			FROM(table.Settings).
			WHERE(table.Settings.Key.EQ(jet.String(string(types.SettingTailnetLock)))),
		&records,
	)
	if err != nil {
		return types.TailnetLockSettings{}, fmt.Errorf("loading the tailnet lock settings: %w", err)
	}

	if len(records) == 0 {
		return types.TailnetLockSettings{}, nil
	}

	var settings types.TailnetLockSettings

	err = json.Unmarshal([]byte(records[0].Setting.Value), &settings)
	if err != nil {
		return types.TailnetLockSettings{}, fmt.Errorf("decoding the tailnet lock settings: %w", err)
	}

	return settings, nil
}

// SaveTailnetLock writes the tailnet lock settings.
func (hsdb *HSDatabase) SaveTailnetLock(settings types.TailnetLockSettings) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encoding the tailnet lock settings: %w", err)
	}

	return hsdb.Write(func(tx *Tx) error {
		return saveSettingValue(tx, types.SettingTailnetLock, string(encoded))
	})
}

// LoadTKAAUMs reads the whole tailnet lock log in commit order.
func (hsdb *HSDatabase) LoadTKAAUMs() ([]types.TKAAUM, error) {
	var records []tkaAUMRecord

	err := hsdb.ex.query(
		jet.SELECT(table.TkaAums.AllColumns).
			FROM(table.TkaAums).
			ORDER_BY(table.TkaAums.CommittedAt.ASC(), table.TkaAums.Hash.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading the tailnet lock log: %w", err)
	}

	out := make([]types.TKAAUM, 0, len(records))

	for _, r := range records {
		raw, err := base64.StdEncoding.DecodeString(r.AUM.Aum)
		if err != nil {
			return nil, fmt.Errorf("tailnet lock AUM %s: %w", r.AUM.Hash, err)
		}

		aum := types.TKAAUM{Hash: r.AUM.Hash, AUM: raw}

		if r.AUM.PrevHash != nil {
			aum.PrevHash = *r.AUM.PrevHash
		}

		if r.AUM.CommittedAt != nil {
			aum.CommittedAt = *r.AUM.CommittedAt
		}

		out = append(out, aum)
	}

	return out, nil
}

// SaveTKAAUMs appends AUMs to the tailnet lock log; one already there
// is left alone.
func (hsdb *HSDatabase) SaveTKAAUMs(aums []types.TKAAUM) error {
	if len(aums) == 0 {
		return nil
	}

	return hsdb.Write(func(tx *Tx) error {
		for _, aum := range aums {
			row := tkaAUMRow{
				Hash:        aum.Hash,
				Aum:         base64.StdEncoding.EncodeToString(aum.AUM),
				CommittedAt: new(aum.CommittedAt.UTC()),
			}

			if aum.PrevHash != "" {
				row.PrevHash = new(aum.PrevHash)
			}

			_, err := tx.executor().exec(
				table.TkaAums.INSERT(table.TkaAums.AllColumns).MODEL(&row).
					ON_CONFLICT(table.TkaAums.Hash).DO_NOTHING(),
			)
			if err != nil {
				return fmt.Errorf("storing tailnet lock AUM %s: %w", aum.Hash, err)
			}
		}

		return nil
	})
}

// DeleteTKAAUMs removes the AUMs with the given hashes from the log.
func (hsdb *HSDatabase) DeleteTKAAUMs(hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}

	values := make([]jet.Expression, len(hashes))
	for i, h := range hashes {
		values[i] = jet.String(h)
	}

	return hsdb.Write(func(tx *Tx) error {
		_, err := tx.executor().exec(table.TkaAums.DELETE().WHERE(table.TkaAums.Hash.IN(values...)))
		if err != nil {
			return fmt.Errorf("deleting tailnet lock AUMs: %w", err)
		}

		return nil
	})
}

// ClearTKAAUMs empties the tailnet lock log.
func (hsdb *HSDatabase) ClearTKAAUMs() error {
	return hsdb.Write(func(tx *Tx) error {
		_, err := tx.executor().exec(table.TkaAums.DELETE().WHERE(table.TkaAums.Hash.IS_NOT_NULL()))
		if err != nil {
			return fmt.Errorf("clearing the tailnet lock log: %w", err)
		}

		return nil
	})
}
