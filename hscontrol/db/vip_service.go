package db

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
	"tailscale.com/tailcfg"
)

// vipServiceRow is a row of the vip_services table; see schema.sql.
type vipServiceRow struct {
	ID          uint64 `sql:"primary_key"`
	Name        string
	DisplayName *string
	Comment     *string
	Ports       *string
	Ipv4        *string
	Ipv6        *string
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

type vipServiceRecord struct {
	VIPService vipServiceRow `alias:"vip_services"`
}

func (r vipServiceRow) service() (types.VIPService, error) {
	svc := types.VIPService{
		ID:   types.VIPServiceID(r.ID),
		Name: tailcfg.ServiceName(r.Name),
	}

	if r.DisplayName != nil {
		svc.DisplayName = *r.DisplayName
	}

	if r.Comment != nil {
		svc.Comment = *r.Comment
	}

	if r.Ports != nil && hasJSONValue(*r.Ports) {
		var ports []string

		err := unmarshalJSONColumn(*r.Ports, &ports)
		if err != nil {
			return types.VIPService{}, fmt.Errorf("service %d ports: %w", r.ID, err)
		}

		svc.Ports, err = types.ParseServicePorts(ports)
		if err != nil {
			return types.VIPService{}, fmt.Errorf("service %d ports: %w", r.ID, err)
		}
	}

	for _, column := range []struct {
		raw  *string
		addr **netip.Addr
	}{{r.Ipv4, &svc.IPv4}, {r.Ipv6, &svc.IPv6}} {
		if column.raw == nil || *column.raw == "" {
			continue
		}

		addr, err := netip.ParseAddr(*column.raw)
		if err != nil {
			return types.VIPService{}, fmt.Errorf("service %d address: %w", r.ID, err)
		}

		*column.addr = &addr
	}

	if r.CreatedAt != nil {
		svc.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		svc.UpdatedAt = *r.UpdatedAt
	}

	return svc, nil
}

func vipServiceRowFrom(svc types.VIPService, now time.Time) (vipServiceRow, error) {
	ports, err := marshalJSONColumn(types.ServicePortsStrings(svc.Ports))
	if err != nil {
		return vipServiceRow{}, err
	}

	row := vipServiceRow{
		ID:          svc.ID.Uint64(),
		Name:        string(svc.Name),
		DisplayName: &svc.DisplayName,
		Comment:     &svc.Comment,
		Ports:       &ports,
		UpdatedAt:   &now,
	}

	if svc.IPv4 != nil {
		v4 := svc.IPv4.String()
		row.Ipv4 = &v4
	}

	if svc.IPv6 != nil {
		v6 := svc.IPv6.String()
		row.Ipv6 = &v6
	}

	return row, nil
}

// ListVIPServices reads every service in name order.
func (hsdb *HSDatabase) ListVIPServices() ([]types.VIPService, error) {
	return listVIPServices(hsdb)
}

func listVIPServices(q Querier) ([]types.VIPService, error) {
	var records []vipServiceRecord

	err := q.executor().query(
		jet.SELECT(table.VipServices.AllColumns).FROM(table.VipServices).ORDER_BY(table.VipServices.Name.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading services: %w", err)
	}

	out := make([]types.VIPService, 0, len(records))

	for _, r := range records {
		svc, err := r.VIPService.service()
		if err != nil {
			return nil, err
		}

		out = append(out, svc)
	}

	return out, nil
}

// GetVIPService reads one service by name.
func (hsdb *HSDatabase) GetVIPService(name tailcfg.ServiceName) (types.VIPService, error) {
	return getVIPService(hsdb, name)
}

func getVIPService(q Querier, name tailcfg.ServiceName) (types.VIPService, error) {
	var records []vipServiceRecord

	err := q.executor().query(
		jet.SELECT(table.VipServices.AllColumns).FROM(table.VipServices).
			WHERE(table.VipServices.Name.EQ(jet.String(string(name)))).LIMIT(1),
		&records,
	)
	if err != nil {
		return types.VIPService{}, fmt.Errorf("loading service %s: %w", name, err)
	}

	if len(records) == 0 {
		return types.VIPService{}, types.ErrVIPServiceNotFound
	}

	return records[0].VIPService.service()
}

// CreateVIPService stores a service with the addresses the caller
// allocated.
func (hsdb *HSDatabase) CreateVIPService(svc types.VIPService) (types.VIPService, error) {
	return Write(hsdb, func(tx *Tx) (types.VIPService, error) {
		now := time.Now().UTC()

		row, err := vipServiceRowFrom(svc, now)
		if err != nil {
			return types.VIPService{}, err
		}

		row.CreatedAt = &now

		_, err = tx.executor().exec(table.VipServices.INSERT(table.VipServices.MutableColumns).MODEL(&row))
		if err != nil {
			if isUniqueViolation(err) {
				return types.VIPService{}, types.ErrVIPServiceNameTaken
			}

			return types.VIPService{}, fmt.Errorf("creating service: %w", err)
		}

		return getVIPService(tx, svc.Name)
	})
}

// UpdateVIPService replaces the display name, comment and ports of the
// service; its name and addresses never change.
func (hsdb *HSDatabase) UpdateVIPService(svc types.VIPService) (types.VIPService, error) {
	return Write(hsdb, func(tx *Tx) (types.VIPService, error) {
		now := time.Now().UTC()

		row, err := vipServiceRowFrom(svc, now)
		if err != nil {
			return types.VIPService{}, err
		}

		affected, err := tx.executor().exec(
			table.VipServices.UPDATE(
				table.VipServices.DisplayName, table.VipServices.Comment, table.VipServices.Ports,
				table.VipServices.UpdatedAt,
			).
				SET(row.DisplayName, row.Comment, row.Ports, row.UpdatedAt).
				WHERE(table.VipServices.Name.EQ(jet.String(string(svc.Name)))),
		)
		if err != nil {
			return types.VIPService{}, fmt.Errorf("updating service %s: %w", svc.Name, err)
		}

		if affected == 0 {
			return types.VIPService{}, types.ErrVIPServiceNotFound
		}

		return getVIPService(tx, svc.Name)
	})
}

// DeleteVIPService removes the service and takes its name out of every
// node's approved list.
func (hsdb *HSDatabase) DeleteVIPService(name tailcfg.ServiceName) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.VipServices.DELETE().WHERE(table.VipServices.Name.EQ(jet.String(string(name)))),
		)
		if err != nil {
			return fmt.Errorf("deleting service %s: %w", name, err)
		}

		if affected == 0 {
			return types.ErrVIPServiceNotFound
		}

		return nil
	})
}
