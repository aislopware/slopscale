package db

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// appConnectorRow is a row of the app_connectors table; see schema.sql.
type appConnectorRow struct {
	ID          uint64 `sql:"primary_key"`
	Name        string
	Description *string
	Domains     *string
	Connectors  *string
	Routes      *string
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

type appConnectorRecord struct {
	AppConnector appConnectorRow `alias:"app_connectors"`
}

func (r appConnectorRow) app() (types.AppConnector, error) {
	app := types.AppConnector{
		ID:   types.AppConnectorID(r.ID),
		Name: r.Name,
	}

	if r.Description != nil {
		app.Description = *r.Description
	}

	for _, column := range []struct {
		raw  *string
		dest *[]string
		what string
	}{{r.Domains, &app.Domains, "domains"}, {r.Connectors, &app.Connectors, "connectors"}} {
		if column.raw == nil {
			continue
		}

		err := unmarshalJSONColumn(*column.raw, column.dest)
		if err != nil {
			return types.AppConnector{}, fmt.Errorf("app %d %s: %w", r.ID, column.what, err)
		}
	}

	if r.Routes != nil && hasJSONValue(*r.Routes) {
		var routes []netip.Prefix

		err := unmarshalJSONColumn(*r.Routes, &routes)
		if err != nil {
			return types.AppConnector{}, fmt.Errorf("app %d routes: %w", r.ID, err)
		}

		app.Routes = routes
	}

	if r.CreatedAt != nil {
		app.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		app.UpdatedAt = *r.UpdatedAt
	}

	return app, nil
}

func appConnectorRowFrom(app types.AppConnector, now time.Time) (appConnectorRow, error) {
	row := appConnectorRow{
		ID:          app.ID.Uint64(),
		Name:        app.Name,
		Description: &app.Description,
		UpdatedAt:   &now,
	}

	for _, column := range []struct {
		value any
		dest  **string
	}{{app.Domains, &row.Domains}, {app.Connectors, &row.Connectors}, {app.Routes, &row.Routes}} {
		encoded, err := marshalJSONColumn(column.value)
		if err != nil {
			return appConnectorRow{}, err
		}

		*column.dest = &encoded
	}

	return row, nil
}

// ListAppConnectors reads every app in name order.
func (hsdb *HSDatabase) ListAppConnectors() ([]types.AppConnector, error) {
	var records []appConnectorRecord

	err := hsdb.executor().query(
		jet.SELECT(table.AppConnectors.AllColumns).FROM(table.AppConnectors).ORDER_BY(table.AppConnectors.Name.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading apps: %w", err)
	}

	out := make([]types.AppConnector, 0, len(records))

	for _, r := range records {
		app, err := r.AppConnector.app()
		if err != nil {
			return nil, err
		}

		out = append(out, app)
	}

	return out, nil
}

func getAppConnector(q Querier, id types.AppConnectorID) (types.AppConnector, error) {
	var records []appConnectorRecord

	err := q.executor().query(
		jet.SELECT(table.AppConnectors.AllColumns).FROM(table.AppConnectors).
			WHERE(table.AppConnectors.ID.EQ(jet.Uint64(id.Uint64()))).LIMIT(1),
		&records,
	)
	if err != nil {
		return types.AppConnector{}, fmt.Errorf("loading app %d: %w", id, err)
	}

	if len(records) == 0 {
		return types.AppConnector{}, types.ErrAppConnectorNotFound
	}

	return records[0].AppConnector.app()
}

// CreateAppConnector stores an app.
func (hsdb *HSDatabase) CreateAppConnector(app types.AppConnector) (types.AppConnector, error) {
	return Write(hsdb, func(tx *Tx) (types.AppConnector, error) {
		now := time.Now().UTC()

		row, err := appConnectorRowFrom(app, now)
		if err != nil {
			return types.AppConnector{}, err
		}

		row.CreatedAt = &now

		id, err := insertReturningID(tx,
			table.AppConnectors.INSERT(table.AppConnectors.MutableColumns).MODEL(&row).
				RETURNING(table.AppConnectors.ID.AS("id_row.id")),
			"app", types.ErrAppConnectorNameTaken)
		if err != nil {
			return types.AppConnector{}, err
		}

		return getAppConnector(tx, types.AppConnectorID(id))
	})
}

// UpdateAppConnector replaces everything but the ID and the creation time.
func (hsdb *HSDatabase) UpdateAppConnector(app types.AppConnector) (types.AppConnector, error) {
	return Write(hsdb, func(tx *Tx) (types.AppConnector, error) {
		now := time.Now().UTC()

		row, err := appConnectorRowFrom(app, now)
		if err != nil {
			return types.AppConnector{}, err
		}

		affected, err := tx.executor().exec(
			table.AppConnectors.UPDATE(
				table.AppConnectors.Name, table.AppConnectors.Description, table.AppConnectors.Domains,
				table.AppConnectors.Connectors, table.AppConnectors.Routes, table.AppConnectors.UpdatedAt,
			).
				SET(row.Name, row.Description, row.Domains, row.Connectors, row.Routes, row.UpdatedAt).
				WHERE(table.AppConnectors.ID.EQ(jet.Uint64(app.ID.Uint64()))),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return types.AppConnector{}, types.ErrAppConnectorNameTaken
			}

			return types.AppConnector{}, fmt.Errorf("updating app %d: %w", app.ID, err)
		}

		if affected == 0 {
			return types.AppConnector{}, types.ErrAppConnectorNotFound
		}

		return getAppConnector(tx, app.ID)
	})
}

// DeleteAppConnector removes an app.
func (hsdb *HSDatabase) DeleteAppConnector(id types.AppConnectorID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.AppConnectors.DELETE().WHERE(table.AppConnectors.ID.EQ(jet.Uint64(id.Uint64()))),
		)
		if err != nil {
			return fmt.Errorf("deleting app %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrAppConnectorNotFound
		}

		return nil
	})
}
