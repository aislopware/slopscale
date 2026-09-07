package db

import (
	"fmt"
	"net/netip"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

// Rows of the network tables; see schema.sql.
type (
	networkRow struct {
		ID          uint64 `sql:"primary_key"`
		Name        string
		Description string
		Enabled     bool
		CreatedAt   *time.Time
		UpdatedAt   *time.Time
	}
	networkPrefixRow struct {
		ID        uint64 `sql:"primary_key"`
		NetworkID uint64
		Prefix    string
	}
	networkRouterRow struct {
		ID        uint64 `sql:"primary_key"`
		NetworkID uint64
		NodeID    uint64
	}
	networkGroupRow struct {
		ID        uint64 `sql:"primary_key"`
		NetworkID uint64
		GroupID   uint64
	}

	networkRecord struct {
		Network networkRow `alias:"networks"`
	}
	networkPrefixRecord struct {
		NetworkPrefix networkPrefixRow `alias:"network_prefixes"`
	}
	networkRouterRecord struct {
		NetworkRouter networkRouterRow `alias:"network_routers"`
	}
	networkGroupRecord struct {
		NetworkGroup networkGroupRow `alias:"network_groups"`
	}
)

func (r networkRow) network() types.Network {
	n := types.Network{
		ID:          types.NetworkID(r.ID),
		Name:        r.Name,
		Description: r.Description,
		Enabled:     r.Enabled,
	}

	if r.CreatedAt != nil {
		n.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		n.UpdatedAt = *r.UpdatedAt
	}

	return n
}

// loadNetworks reads every network with its prefixes, routers and
// groups, in ID order.
func loadNetworks(q Querier) ([]types.Network, error) {
	var (
		networks []networkRecord
		prefixes []networkPrefixRecord
		routers  []networkRouterRecord
		groups   []networkGroupRecord
	)

	ex := q.executor()

	err := ex.query(
		jet.SELECT(table.Networks.AllColumns).FROM(table.Networks).ORDER_BY(table.Networks.ID.ASC()),
		&networks,
	)
	if err != nil {
		return nil, fmt.Errorf("loading networks: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.NetworkPrefixes.AllColumns).
			FROM(table.NetworkPrefixes).
			ORDER_BY(table.NetworkPrefixes.ID.ASC()),
		&prefixes,
	)
	if err != nil {
		return nil, fmt.Errorf("loading network prefixes: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.NetworkRouters.AllColumns).
			FROM(table.NetworkRouters).
			ORDER_BY(table.NetworkRouters.NodeID.ASC()),
		&routers,
	)
	if err != nil {
		return nil, fmt.Errorf("loading network routers: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.NetworkGroups.AllColumns).
			FROM(table.NetworkGroups).
			ORDER_BY(table.NetworkGroups.GroupID.ASC()),
		&groups,
	)
	if err != nil {
		return nil, fmt.Errorf("loading network groups: %w", err)
	}

	out := make([]types.Network, 0, len(networks))
	idx := make(map[uint64]int, len(networks))

	for i, r := range networks {
		out = append(out, r.Network.network())
		idx[r.Network.ID] = i
	}

	for _, r := range prefixes {
		i, ok := idx[r.NetworkPrefix.NetworkID]
		if !ok {
			continue
		}

		prefix, err := netip.ParsePrefix(r.NetworkPrefix.Prefix)
		if err != nil {
			return nil, fmt.Errorf(
				"network %d holds prefix %q: %w",
				r.NetworkPrefix.NetworkID,
				r.NetworkPrefix.Prefix,
				err,
			)
		}

		out[i].Prefixes = append(out[i].Prefixes, prefix)
	}

	for _, r := range routers {
		if i, ok := idx[r.NetworkRouter.NetworkID]; ok {
			out[i].RouterNodeIDs = append(out[i].RouterNodeIDs, types.NodeID(r.NetworkRouter.NodeID))
		}
	}

	for _, r := range groups {
		if i, ok := idx[r.NetworkGroup.NetworkID]; ok {
			out[i].GroupIDs = append(out[i].GroupIDs, types.GroupID(r.NetworkGroup.GroupID))
		}
	}

	return out, nil
}

// CreateNetwork stores a network with its prefixes, routers and groups.
func (hsdb *HSDatabase) CreateNetwork(network types.Network) (types.Network, error) {
	return Write(hsdb, func(tx *Tx) (types.Network, error) {
		now := time.Now().UTC()
		row := networkRow{
			Name:        network.Name,
			Description: network.Description,
			Enabled:     network.Enabled,
			CreatedAt:   &now,
			UpdatedAt:   &now,
		}

		var inserted idRow

		err := tx.executor().query(
			table.Networks.INSERT(table.Networks.MutableColumns).MODEL(&row).
				RETURNING(table.Networks.ID.AS("id_row.id")),
			&inserted,
		)
		if err != nil {
			if isUniqueViolation(err) {
				return types.Network{}, types.ErrNetworkNameTaken
			}

			return types.Network{}, fmt.Errorf("creating network: %w", err)
		}

		id := types.NetworkID(inserted.ID)

		err = setNetworkMembers(tx, id, network)
		if err != nil {
			return types.Network{}, err
		}

		return getNetwork(tx, id)
	})
}

// UpdateNetwork replaces every field of the network.
func (hsdb *HSDatabase) UpdateNetwork(network types.Network) (types.Network, error) {
	return Write(hsdb, func(tx *Tx) (types.Network, error) {
		now := time.Now().UTC()

		affected, err := tx.executor().exec(
			table.Networks.UPDATE(
				table.Networks.Name, table.Networks.Description, table.Networks.Enabled, table.Networks.UpdatedAt,
			).SET(
				network.Name, network.Description, network.Enabled, now,
			).WHERE(table.Networks.ID.EQ(jet.Uint64(uint64(network.ID)))),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return types.Network{}, types.ErrNetworkNameTaken
			}

			return types.Network{}, fmt.Errorf("updating network %d: %w", network.ID, err)
		}

		if affected == 0 {
			return types.Network{}, types.ErrNetworkNotFound
		}

		id := jet.Uint64(uint64(network.ID))

		for _, del := range []jet.DeleteStatement{
			table.NetworkPrefixes.DELETE().WHERE(table.NetworkPrefixes.NetworkID.EQ(id)),
			table.NetworkRouters.DELETE().WHERE(table.NetworkRouters.NetworkID.EQ(id)),
			table.NetworkGroups.DELETE().WHERE(table.NetworkGroups.NetworkID.EQ(id)),
		} {
			_, err = tx.executor().exec(del)
			if err != nil {
				return types.Network{}, fmt.Errorf("clearing members of network %d: %w", network.ID, err)
			}
		}

		err = setNetworkMembers(tx, network.ID, network)
		if err != nil {
			return types.Network{}, err
		}

		return getNetwork(tx, network.ID)
	})
}

func setNetworkMembers(q Querier, id types.NetworkID, network types.Network) error {
	nid := uint64(id)

	if len(network.Prefixes) > 0 {
		rows := make([]networkPrefixRow, 0, len(network.Prefixes))
		for _, p := range network.Prefixes {
			rows = append(rows, networkPrefixRow{NetworkID: nid, Prefix: p.String()})
		}

		_, err := q.executor().exec(table.NetworkPrefixes.INSERT(table.NetworkPrefixes.MutableColumns).MODELS(rows))
		if err != nil {
			return fmt.Errorf("storing prefixes of network %d: %w", id, err)
		}
	}

	if routers := dedupe(network.RouterNodeIDs); len(routers) > 0 {
		rows := make([]networkRouterRow, 0, len(routers))
		for _, n := range routers {
			rows = append(rows, networkRouterRow{NetworkID: nid, NodeID: n.Uint64()})
		}

		_, err := q.executor().exec(table.NetworkRouters.INSERT(table.NetworkRouters.MutableColumns).MODELS(rows))
		if err != nil {
			return fmt.Errorf("storing routers of network %d: %w", id, err)
		}
	}

	if groups := dedupe(network.GroupIDs); len(groups) > 0 {
		rows := make([]networkGroupRow, 0, len(groups))
		for _, g := range groups {
			rows = append(rows, networkGroupRow{NetworkID: nid, GroupID: uint64(g)})
		}

		_, err := q.executor().exec(table.NetworkGroups.INSERT(table.NetworkGroups.MutableColumns).MODELS(rows))
		if err != nil {
			return fmt.Errorf("storing groups of network %d: %w", id, err)
		}
	}

	return nil
}

// GetNetwork reads one network with its members.
func (hsdb *HSDatabase) GetNetwork(id types.NetworkID) (types.Network, error) {
	return Read(hsdb, func(rx *Tx) (types.Network, error) {
		return getNetwork(rx, id)
	})
}

func getNetwork(q Querier, id types.NetworkID) (types.Network, error) {
	networks, err := loadNetworks(q)
	if err != nil {
		return types.Network{}, err
	}

	for _, n := range networks {
		if n.ID == id {
			return n, nil
		}
	}

	return types.Network{}, types.ErrNetworkNotFound
}

// SetNetworkEnabled switches the network on or off.
func (hsdb *HSDatabase) SetNetworkEnabled(id types.NetworkID, enabled bool) (types.Network, error) {
	return Write(hsdb, func(tx *Tx) (types.Network, error) {
		affected, err := tx.executor().exec(
			table.Networks.UPDATE(table.Networks.Enabled, table.Networks.UpdatedAt).
				SET(enabled, time.Now().UTC()).
				WHERE(table.Networks.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return types.Network{}, fmt.Errorf("switching network %d: %w", id, err)
		}

		if affected == 0 {
			return types.Network{}, types.ErrNetworkNotFound
		}

		return getNetwork(tx, id)
	})
}

// DeleteNetwork removes a network and its members.
func (hsdb *HSDatabase) DeleteNetwork(id types.NetworkID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.Networks.DELETE().WHERE(table.Networks.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting network %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrNetworkNotFound
		}

		return nil
	})
}
