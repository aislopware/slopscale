package db

import (
	"errors"
	"fmt"
	"os"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
)

// SetPolicy sets the policy in the database.
func (hsdb *HSDatabase) SetPolicy(policy string) (*types.Policy, error) {
	now := time.Now()

	p := types.Policy{
		CreatedAt: now,
		UpdatedAt: now,
		Data:      policy,
	}

	var inserted idRow

	err := hsdb.ex.query(
		table.Policies.INSERT(table.Policies.MutableColumns).MODEL(&p).
			RETURNING(table.Policies.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return nil, err
	}

	p.ID = uint(inserted.ID)

	return &p, nil
}

// GetPolicy returns the latest policy in the database.
func (hsdb *HSDatabase) GetPolicy() (*types.Policy, error) {
	return GetPolicy(hsdb)
}

// GetPolicy returns the latest policy from the database.
// This standalone function can be used in contexts where [HSDatabase] is not available,
// such as during migrations.
func GetPolicy(q Querier) (*types.Policy, error) {
	var record policyRecord

	err := q.executor().query(
		jet.SELECT(table.Policies.AllColumns).FROM(table.Policies).
			WHERE(table.Policies.DeletedAt.IS_NULL()).
			ORDER_BY(table.Policies.ID.DESC()).LIMIT(1),
		&record,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, types.ErrPolicyNotFound
		}

		return nil, err
	}

	return &record.Policy, nil
}

// PolicyBytes loads policy configuration from file or database based on the configured mode.
// Returns nil if no policy is configured, which is valid.
// This standalone function can be used in contexts where [HSDatabase] is not available,
// such as during migrations.
func PolicyBytes(q Querier, cfg *types.Config) ([]byte, error) {
	switch cfg.Policy.Mode {
	case types.PolicyModeFile:
		path := cfg.Policy.Path

		// It is fine to start headscale without a policy file.
		if path == "" {
			return nil, nil
		}

		absPath := util.AbsolutePathFromConfigPath(path)

		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("reading policy file %q: %w", absPath, err)
		}

		return data, nil

	case types.PolicyModeDB:
		p, err := GetPolicy(q)
		if err != nil {
			if errors.Is(err, types.ErrPolicyNotFound) {
				return nil, nil
			}

			return nil, err
		}

		if p.Data == "" {
			return nil, nil
		}

		return []byte(p.Data), nil
	}

	return nil, nil
}
