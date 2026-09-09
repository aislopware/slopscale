package db

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/aislopware/slopscale/hscontrol/util/zlog/zf"
	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/rs/zerolog/log"
	"tailscale.com/types/key"
	"tailscale.com/util/dnsname"
)

const (
	NodeGivenNameHashLength = 8
	NodeGivenNameTrimSize   = 2

	// defaultTestNodePrefix is the default hostname prefix for nodes created in tests.
	defaultTestNodePrefix = "testnode"
)

// ErrNodeNameNotUnique is returned when a node name is not unique.
var ErrNodeNameNotUnique = errors.New("node name is not unique")

var (
	ErrNodeNotFound                  = errors.New("node not found")
	ErrNodeRouteIsNotAvailable       = errors.New("route is not available on node")
	ErrNodeNotFoundRegistrationCache = errors.New(
		"node not found in registration cache",
	)
	ErrCouldNotConvertNodeInterface = errors.New("failed to convert node interface")
)

// Aliased tables for the pre-auth key a node was registered with and that
// key's user, joined next to the node's own user.
var (
	authKeyTable     = table.PreAuthKeys.AS("auth_key")
	authKeyUserTable = table.Users.AS("auth_key_user")
)

// selectNodes is the base query for nodes with their user, their pre-auth
// key and the key's user joined in, ordered by id.
func selectNodes() jet.SelectStatement {
	return jet.SELECT(
		table.Nodes.AllColumns,
		table.Users.AllColumns,
		authKeyTable.AllColumns,
		authKeyUserTable.AllColumns,
	).FROM(
		table.Nodes.
			LEFT_JOIN(table.Users, table.Nodes.UserID.EQ(table.Users.ID).AND(table.Users.DeletedAt.IS_NULL())).
			LEFT_JOIN(authKeyTable, table.Nodes.AuthKeyID.EQ(authKeyTable.ID)).
			LEFT_JOIN(authKeyUserTable, authKeyTable.UserID.EQ(authKeyUserTable.ID).
				AND(authKeyUserTable.DeletedAt.IS_NULL())),
	).ORDER_BY(table.Nodes.ID.ASC())
}

// listNodesWithoutAuthKeys loads nodes with their users only, with the
// user columns of before 202609062100-user-role. Migrations that predate
// columns of the pre_auth_keys or users tables use it.
// nodeColumnsBeforeApproval is the nodes table as it was before
// 202609070900-approval.
var nodeColumnsBeforeApproval = jet.ColumnList{
	table.Nodes.ID,
	table.Nodes.MachineKey,
	table.Nodes.NodeKey,
	table.Nodes.DiscoKey,
	table.Nodes.Endpoints,
	table.Nodes.HostInfo,
	table.Nodes.Ipv4,
	table.Nodes.Ipv6,
	table.Nodes.Hostname,
	table.Nodes.GivenName,
	table.Nodes.UserID,
	table.Nodes.RegisterMethod,
	table.Nodes.Tags,
	table.Nodes.AuthKeyID,
	table.Nodes.LastSeen,
	table.Nodes.Expiry,
	table.Nodes.ApprovedRoutes,
	table.Nodes.CreatedAt,
	table.Nodes.UpdatedAt,
	table.Nodes.DeletedAt,
}

func listNodesWithoutAuthKeys(q Querier) (types.Nodes, error) {
	stmt := jet.SELECT(nodeColumnsBeforeApproval, userColumnsBeforeRoles).
		FROM(table.Nodes.LEFT_JOIN(table.Users, table.Nodes.UserID.EQ(table.Users.ID))).
		ORDER_BY(table.Nodes.ID.ASC())

	var records []nodeRecord

	err := q.executor().query(stmt, &records)
	if err != nil {
		return nil, err
	}

	return nodeRecordsToNodes(records)
}

func queryNodes(q Querier, stmt jet.SelectStatement) (types.Nodes, error) {
	var records []nodeRecord

	err := q.executor().query(stmt, &records)
	if err != nil {
		return nil, err
	}

	return nodesWithShares(q, records)
}

func queryNode(q Querier, stmt jet.SelectStatement) (*types.Node, error) {
	var record nodeRecord

	err := q.executor().query(stmt.LIMIT(1), &record)
	if err != nil {
		return nil, err
	}

	return nodeWithShares(q, &record)
}

// Node statements on the map request and registration paths, rendered once;
// see [fixedSQL].
var (
	nodeByID = newFixedSQL(func() statement {
		return selectNodes().WHERE(table.Nodes.ID.EQ(jet.Uint64(0))).LIMIT(1)
	})
	nodeByNodeKey = newFixedSQL(func() statement {
		return selectNodes().WHERE(table.Nodes.NodeKey.EQ(jet.String(""))).LIMIT(1)
	})
	allNodes    = newFixedSQL(func() statement { return selectNodes() })
	peersOfNode = newFixedSQL(func() statement {
		return selectNodes().WHERE(table.Nodes.ID.NOT_EQ(jet.Uint64(0)))
	})
	nodeLastSeen = newFixedSQL(func() statement {
		return table.Nodes.UPDATE(table.Nodes.LastSeen, table.Nodes.UpdatedAt).
			SET(jet.String(""), jet.String("")).
			WHERE(table.Nodes.ID.EQ(jet.Uint64(0)))
	})
)

// limitOne is the argument jet binds for LIMIT(1).
const limitOne = int64(1)

func fixedNodes(q Querier, stmt *fixedSQL, args ...any) (types.Nodes, error) {
	var records []nodeRecord

	err := q.executor().queryFixed(stmt, &records, args...)
	if err != nil {
		return nil, err
	}

	return nodesWithShares(q, records)
}

func fixedNode(q Querier, stmt *fixedSQL, args ...any) (*types.Node, error) {
	var record nodeRecord

	err := q.executor().queryFixed(stmt, &record, args...)
	if err != nil {
		return nil, err
	}

	return nodeWithShares(q, &record)
}

// nodesWithShares converts the records and attaches their shares and
// attributes; every production node read goes through it or
// [nodeWithShares].
func nodesWithShares(q Querier, records []nodeRecord) (types.Nodes, error) {
	nodes, err := nodeRecordsToNodes(records)
	if err != nil {
		return nil, err
	}

	err = attachShares(q, nodes)
	if err != nil {
		return nil, err
	}

	return nodes, attachAttributes(q, nodes)
}

func nodeWithShares(q Querier, record *nodeRecord) (*types.Node, error) {
	node, err := record.node()
	if err != nil {
		return nil, err
	}

	err = attachSharesToNode(q, node)
	if err != nil {
		return nil, err
	}

	return node, attachAttributesToNode(q, node)
}

func nodeIDList(ids []types.NodeID) []jet.Expression {
	exprs := make([]jet.Expression, len(ids))
	for i, id := range ids {
		exprs[i] = jet.Uint64(id.Uint64())
	}

	return exprs
}

// ListPeers returns peers of node, regardless of any Policy or if the node is expired.
// If no peer IDs are given, all peers are returned.
// If at least one peer ID is given, only these peer nodes will be returned.
func (hsdb *HSDatabase) ListPeers(nodeID types.NodeID, peerIDs ...types.NodeID) (types.Nodes, error) {
	return ListPeers(hsdb, nodeID, peerIDs...)
}

// ListPeers returns peers of node, regardless of any Policy or if the node is expired.
// If no peer IDs are given, all peers are returned.
// If at least one peer ID is given, only these peer nodes will be returned.
func ListPeers(q Querier, nodeID types.NodeID, peerIDs ...types.NodeID) (types.Nodes, error) {
	var (
		nodes types.Nodes
		err   error
	)

	if len(peerIDs) > 0 {
		where := table.Nodes.ID.NOT_EQ(jet.Uint64(nodeID.Uint64())).AND(table.Nodes.ID.IN(nodeIDList(peerIDs)...))
		nodes, err = queryNodes(q, selectNodes().WHERE(where))
	} else {
		nodes, err = fixedNodes(q, peersOfNode, nodeID.Uint64())
	}

	if err != nil {
		return types.Nodes{}, err
	}

	return nodes, nil
}

// ListNodes queries the database for either all nodes if no parameters are given
// or for the given nodes if at least one node ID is given as parameter.
func (hsdb *HSDatabase) ListNodes(nodeIDs ...types.NodeID) (types.Nodes, error) {
	return ListNodes(hsdb, nodeIDs...)
}

// ListNodes queries the database for either all nodes if no parameters are given
// or for the given nodes if at least one node ID is given as parameter.
func ListNodes(q Querier, nodeIDs ...types.NodeID) (types.Nodes, error) {
	if len(nodeIDs) > 0 {
		return queryNodes(q, selectNodes().WHERE(table.Nodes.ID.IN(nodeIDList(nodeIDs)...)))
	}

	return fixedNodes(q, allNodes)
}

// ListEphemeralNodes returns the nodes registered with an ephemeral
// pre-auth key or ephemeral by their own request.
func (hsdb *HSDatabase) ListEphemeralNodes() (types.Nodes, error) {
	return Read(hsdb, func(rx *Tx) (types.Nodes, error) {
		return queryNodes(rx, selectNodes().WHERE(
			authKeyTable.Ephemeral.EQ(jet.Bool(true)).OR(table.Nodes.Ephemeral.EQ(jet.Bool(true))),
		))
	})
}

// getNode finds a node by owner and hostname. Only tests use it, but it
// stays a query so those tests exercise the same joins as production reads.
func (hsdb *HSDatabase) getNode(uid types.UserID, name string) (*types.Node, error) {
	return queryNode(hsdb, selectNodes().WHERE(
		table.Nodes.UserID.EQ(jet.Uint64(uint64(uid))).AND(table.Nodes.Hostname.EQ(jet.String(name))),
	))
}

func (hsdb *HSDatabase) GetNodeByID(id types.NodeID) (*types.Node, error) {
	return GetNodeByID(hsdb, id)
}

// GetNodeByID finds a [types.Node] by ID and returns the [types.Node] struct.
func GetNodeByID(q Querier, id types.NodeID) (*types.Node, error) {
	return fixedNode(q, nodeByID, id.Uint64(), limitOne)
}

func (hsdb *HSDatabase) GetNodeByNodeKey(nodeKey key.NodePublic) (*types.Node, error) {
	return GetNodeByNodeKey(hsdb, nodeKey)
}

// GetNodeByNodeKey finds a [types.Node] by its [key.NodePublic] and returns the [types.Node] struct.
func GetNodeByNodeKey(
	q Querier,
	nodeKey key.NodePublic,
) (*types.Node, error) {
	return fixedNode(q, nodeByNodeKey, nodeKey.String(), limitOne)
}

// CreateNode inserts node and sets its ID. CreatedAt and UpdatedAt are
// stamped with the current time when unset.
func CreateNode(q Querier, node *types.Node) error {
	now := time.Now()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = now
	}

	if node.UpdatedAt.IsZero() {
		node.UpdatedAt = now
	}

	row, err := nodeRowFrom(node)
	if err != nil {
		return err
	}

	columns := table.Nodes.MutableColumns
	if node.ID != 0 {
		columns = table.Nodes.AllColumns
	}

	var inserted idRow

	err = q.executor().query(
		table.Nodes.INSERT(columns).MODEL(row).RETURNING(table.Nodes.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return err
	}

	node.ID = types.NodeID(inserted.ID)

	return nil
}

// NodeUpdate selects which optional columns [UpdateNode] writes on top of
// the node state it always writes: keys, endpoints, host info, addresses,
// names, user, registration method, tags, last seen and approved routes.
type NodeUpdate struct {
	// Expiry writes the expiry column. Map request updates leave it out:
	// expiry only changes through explicit expiry updates or
	// re-registration.
	Expiry bool
	// AuthKey writes auth_key_id. Left out on map request updates so a
	// deleted key's stale reference is never persisted (#2862); written
	// on registration, which presents a freshly validated key, and when
	// clearing the key, which can never violate the foreign key.
	AuthKey bool
}

// UpdateNode writes node's state to its row by id and stamps UpdatedAt.
// Every selected column is written, including nil and zero values, so a
// node converted from user-owned to tagged persists its cleared user.
func UpdateNode(q Querier, node *types.Node, update NodeUpdate) error {
	node.UpdatedAt = time.Now()

	row, err := nodeRowFrom(node)
	if err != nil {
		return err
	}

	stmt := nodeUpdates[boolIndex(update.Expiry)][boolIndex(update.AuthKey)]

	return q.executor().execFixed(stmt, row.updateArgs(update)...)
}

// nodeUpdates holds the four shapes of [UpdateNode], indexed by whether
// the expiry and then the auth key are written.
var nodeUpdates = [2][2]*fixedSQL{
	{
		newFixedSQL(nodeUpdateStatement(NodeUpdate{})),
		newFixedSQL(nodeUpdateStatement(NodeUpdate{AuthKey: true})),
	},
	{
		newFixedSQL(nodeUpdateStatement(NodeUpdate{Expiry: true})),
		newFixedSQL(nodeUpdateStatement(NodeUpdate{Expiry: true, AuthKey: true})),
	},
}

func boolIndex(b bool) int {
	if b {
		return 1
	}

	return 0
}

// nodeUpdateColumns lists the columns [UpdateNode] writes, in the order
// [nodeRow.updateArgs] supplies their values.
func nodeUpdateColumns(update NodeUpdate) jet.ColumnList {
	columns := jet.ColumnList{
		table.Nodes.MachineKey,
		table.Nodes.NodeKey,
		table.Nodes.DiscoKey,
		table.Nodes.Endpoints,
		table.Nodes.HostInfo,
		table.Nodes.Ipv4,
		table.Nodes.Ipv6,
		table.Nodes.Hostname,
		table.Nodes.GivenName,
		table.Nodes.UserID,
		table.Nodes.RegisterMethod,
		table.Nodes.Tags,
		table.Nodes.LastSeen,
		table.Nodes.ApprovedRoutes,
		table.Nodes.UpdatedAt,
	}

	if update.Expiry {
		columns = append(columns, table.Nodes.Expiry)
	}

	if update.AuthKey {
		columns = append(columns, table.Nodes.AuthKeyID)
	}

	return columns
}

func nodeUpdateStatement(update NodeUpdate) func() statement {
	return func() statement {
		columns := nodeUpdateColumns(update)

		values := make([]any, len(columns))
		for i := range values {
			values[i] = jet.String("")
		}

		return table.Nodes.UPDATE(columns).
			SET(values[0], values[1:]...).
			WHERE(table.Nodes.ID.EQ(jet.Uint64(0)))
	}
}

// updateArgs returns the row's values for [nodeUpdateColumns], followed by
// the ID the WHERE clause binds.
func (r *nodeRow) updateArgs(update NodeUpdate) []any {
	args := []any{
		r.MachineKey,
		r.NodeKey,
		r.DiscoKey,
		r.Endpoints,
		r.HostInfo,
		optional(r.Ipv4),
		optional(r.Ipv6),
		r.Hostname,
		r.GivenName,
		optional(r.UserID),
		r.RegisterMethod,
		r.Tags,
		optional(r.LastSeen),
		r.ApprovedRoutes,
		r.UpdatedAt,
	}

	if update.Expiry {
		args = append(args, optional(r.Expiry))
	}

	if update.AuthKey {
		args = append(args, optional(r.AuthKeyID))
	}

	return append(args, r.ID)
}

// SaveNode overwrites every column of node's row when a row with its ID
// exists and inserts it otherwise, keeping an explicit ID.
func SaveNode(q Querier, node *types.Node) error {
	if node.ID == 0 {
		return CreateNode(q, node)
	}

	node.UpdatedAt = time.Now()

	row, err := nodeRowFrom(node)
	if err != nil {
		return err
	}

	affected, err := q.executor().exec(
		table.Nodes.UPDATE(table.Nodes.MutableColumns).MODEL(row).
			WHERE(table.Nodes.ID.EQ(jet.Uint64(node.ID.Uint64()))),
	)
	if err != nil || affected > 0 {
		return err
	}

	return CreateNode(q, node)
}

// updateNodeColumn sets a single column of a node and stamps updated_at.
func updateNodeColumn(q Querier, nodeID types.NodeID, column jet.Column, value any) error {
	_, err := q.executor().exec(
		table.Nodes.UPDATE(column, table.Nodes.UpdatedAt).
			SET(value, time.Now()).
			WHERE(table.Nodes.ID.EQ(jet.Uint64(nodeID.Uint64()))),
	)

	return err
}

// SetLastSeen sets a node's last seen field indicating that we
// have recently communicating with this node.
func (hsdb *HSDatabase) SetLastSeen(nodeID types.NodeID, lastSeen time.Time) error {
	return hsdb.Write(func(tx *Tx) error {
		return SetLastSeen(tx, nodeID, lastSeen)
	})
}

// SetLastSeen sets a node's last seen field indicating that we
// have recently communicating with this node.
func SetLastSeen(q Querier, nodeID types.NodeID, lastSeen time.Time) error {
	return q.executor().execFixed(nodeLastSeen, lastSeen, time.Now(), nodeID.Uint64())
}

// RenameNode takes a [types.Node] struct and a new [types.Node.GivenName] for the nodes
// and renames it. Validation should be done in the state layer before calling this function.
func RenameNode(q Querier,
	nodeID types.NodeID, newName string,
) error {
	err := dnsname.ValidLabel(newName)
	if err != nil {
		return fmt.Errorf("renaming node: %w", err)
	}

	// Check if the new name is unique
	var count struct{ Count int64 }

	err = q.executor().query(
		jet.SELECT(jet.COUNT(jet.STAR).AS("count")).FROM(table.Nodes).
			WHERE(table.Nodes.GivenName.EQ(jet.String(newName)).
				AND(table.Nodes.ID.NOT_EQ(jet.Uint64(nodeID.Uint64())))),
		&count,
	)
	if err != nil {
		return fmt.Errorf("checking name uniqueness: %w", err)
	}

	if count.Count > 0 {
		return ErrNodeNameNotUnique
	}

	err = updateNodeColumn(q, nodeID, table.Nodes.GivenName, newName)
	if err != nil {
		return fmt.Errorf("renaming node in database: %w", err)
	}

	return nil
}

func (hsdb *HSDatabase) NodeSetExpiry(nodeID types.NodeID, expiry *time.Time) error {
	return hsdb.Write(func(tx *Tx) error {
		return NodeSetExpiry(tx, nodeID, expiry)
	})
}

// NodeSetExpiry sets a new expiry time for a node.
// If expiry is nil, the node's expiry is disabled (node will never expire).
func NodeSetExpiry(q Querier, nodeID types.NodeID, expiry *time.Time) error {
	return updateNodeColumn(q, nodeID, table.Nodes.Expiry, expiry)
}

func (hsdb *HSDatabase) NodeSetApproval(nodeID types.NodeID, approvedAt *time.Time) error {
	return hsdb.Write(func(tx *Tx) error {
		return NodeSetApproval(tx, nodeID, approvedAt)
	})
}

// NodeSetGlobalExitNode records whether the node is a global exit node.
func (hsdb *HSDatabase) NodeSetGlobalExitNode(nodeID types.NodeID, on bool) error {
	return hsdb.Write(func(tx *Tx) error {
		return updateNodeColumn(tx, nodeID, table.Nodes.GlobalExitNode, on)
	})
}

// NodeSetPosture stores the device identity the client reported.
func (hsdb *HSDatabase) NodeSetPosture(nodeID types.NodeID, posture *types.PostureIdentity) error {
	var column *string

	if posture != nil {
		encoded, err := marshalJSONColumn(posture)
		if err != nil {
			return err
		}

		column = &encoded
	}

	return hsdb.Write(func(tx *Tx) error {
		return updateNodeColumn(tx, nodeID, table.Nodes.Posture, column)
	})
}

// NodeSetSuspension records when an administrator suspended the node;
// nil lifts the suspension.
func (hsdb *HSDatabase) NodeSetSuspension(nodeID types.NodeID, suspendedAt *time.Time) error {
	return hsdb.Write(func(tx *Tx) error {
		return updateNodeColumn(tx, nodeID, table.Nodes.SuspendedAt, suspendedAt)
	})
}

// NodeSetApproval records when a node was admitted to the tailnet; nil
// withdraws the approval so the node waits for an administrator again.
func NodeSetApproval(q Querier, nodeID types.NodeID, approvedAt *time.Time) error {
	return updateNodeColumn(q, nodeID, table.Nodes.ApprovedAt, approvedAt)
}

// ApproveAllNodes admits every node still waiting for approval, as when
// device approval is switched off. It returns the affected node ids.
func ApproveAllNodes(q Querier, approvedAt time.Time) ([]types.NodeID, error) {
	var rows []idRow

	err := q.executor().query(
		jet.SELECT(table.Nodes.ID.AS("id_row.id")).FROM(table.Nodes).WHERE(table.Nodes.ApprovedAt.IS_NULL()),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("listing unapproved nodes: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	_, err = q.executor().exec(
		table.Nodes.UPDATE(table.Nodes.ApprovedAt).SET(approvedAt).WHERE(table.Nodes.ApprovedAt.IS_NULL()),
	)
	if err != nil {
		return nil, fmt.Errorf("approving nodes: %w", err)
	}

	ids := make([]types.NodeID, len(rows))
	for i, r := range rows {
		ids[i] = types.NodeID(r.ID)
	}

	return ids, nil
}

func (hsdb *HSDatabase) DeleteNode(node *types.Node) error {
	return hsdb.Write(func(tx *Tx) error {
		return DeleteNode(tx, node)
	})
}

// DeleteNode deletes a [types.Node] from the database.
// Caller is responsible for notifying all of change.
func DeleteNode(q Querier,
	node *types.Node,
) error {
	return deleteNodeByID(q, node.ID)
}

// DeleteEphemeralNode deletes a [types.Node] from the database, note that this method
// will remove it straight, and not notify any changes or consider any routes.
// It is intended for Ephemeral nodes.
func (hsdb *HSDatabase) DeleteEphemeralNode(
	nodeID types.NodeID,
) error {
	return hsdb.Write(func(tx *Tx) error {
		return deleteNodeByID(tx, nodeID)
	})
}

func deleteNodeByID(q Querier, nodeID types.NodeID) error {
	_, err := q.executor().exec(table.Nodes.DELETE().WHERE(table.Nodes.ID.EQ(jet.Uint64(nodeID.Uint64()))))

	return err
}

// RegisterNodeForTest is used only for testing purposes to register a node directly in the database.
// Production code should use [state.State.HandleNodeFromAuthPath] or [state.State.HandleNodeFromPreAuthKey].
func RegisterNodeForTest(q Querier, node types.Node, ipv4, ipv6 *netip.Addr) (*types.Node, error) {
	if !testing.Testing() {
		panic("RegisterNodeForTest can only be called during tests")
	}

	logEvent := log.Debug().
		Str(zf.NodeHostname, node.Hostname).
		Str(zf.MachineKey, node.MachineKey.ShortString()).
		Str(zf.NodeKey, node.NodeKey.ShortString())

	switch {
	case node.User != nil:
		logEvent = logEvent.Str(zf.UserName, node.User.Username())
	case node.UserID != nil:
		logEvent = logEvent.Uint(zf.UserID, *node.UserID)
	default:
		logEvent = logEvent.Str(zf.UserName, "none")
	}

	logEvent.Msg("registering test node")

	// Reuse the existing node's identity only when the same machine
	// re-registers for the same user; a different user is a new node. Match on
	// (machine_key, user_id) precisely - a machine key can map to several nodes
	// (one per user), so a machine-key-only lookup would be ambiguous.
	if node.UserID != nil {
		oldNode, err := queryNode(q, selectNodes().WHERE(
			table.Nodes.MachineKey.EQ(jet.String(node.MachineKey.String())).
				AND(table.Nodes.UserID.EQ(jet.Uint64(uint64(*node.UserID)))),
		))
		if err == nil {
			node.ID = oldNode.ID
			node.GivenName = oldNode.GivenName
			node.ApprovedRoutes = oldNode.ApprovedRoutes
			// Don't overwrite the provided IPs with old ones when they exist
			if ipv4 == nil {
				ipv4 = oldNode.IPv4
			}

			if ipv6 == nil {
				ipv6 = oldNode.IPv6
			}
		}
	}

	// If the node exists and it already has IP(s), we just save it
	// so we store the node.Expire and node.Nodekey that has been set when
	// adding it to the registrationCache
	if node.IPv4 != nil || node.IPv6 != nil {
		err := SaveNode(q, &node)
		if err != nil {
			return nil, fmt.Errorf("registering existing node in database: %w", err)
		}

		log.Trace().
			Caller().
			Str(zf.NodeHostname, node.Hostname).
			Str(zf.MachineKey, node.MachineKey.ShortString()).
			Str(zf.NodeKey, node.NodeKey.ShortString()).
			Str(zf.UserName, node.User.Username()).
			Msg("Test node authorized again")

		return &node, nil
	}

	node.IPv4 = ipv4
	node.IPv6 = ipv6

	if node.GivenName == "" {
		node.GivenName = dnsname.SanitizeHostname(node.Hostname)
		if node.GivenName == "" {
			node.GivenName = "node"
		}
	}

	err := SaveNode(q, &node)
	if err != nil {
		return nil, fmt.Errorf("saving node to database: %w", err)
	}

	log.Trace().
		Caller().
		Str(zf.NodeHostname, node.Hostname).
		Msg("Test node registered with the database")

	return &node, nil
}

// NodeSetNodeKey sets the node key of a node and saves it to the database.
func NodeSetNodeKey(q Querier, node *types.Node, nodeKey key.NodePublic) error {
	err := updateNodeColumn(q, node.ID, table.Nodes.NodeKey, nodeKey.String())
	if err != nil {
		return err
	}

	node.NodeKey = nodeKey

	return nil
}

func (hsdb *HSDatabase) NodeSetMachineKey(
	node *types.Node,
	machineKey key.MachinePublic,
) error {
	return hsdb.Write(func(tx *Tx) error {
		return NodeSetMachineKey(tx, node, machineKey)
	})
}

// NodeSetMachineKey sets the node key of a node and saves it to the database.
func NodeSetMachineKey(
	q Querier,
	node *types.Node,
	machineKey key.MachinePublic,
) error {
	err := updateNodeColumn(q, node.ID, table.Nodes.MachineKey, machineKey.String())
	if err != nil {
		return err
	}

	node.MachineKey = machineKey

	return nil
}

// EphemeralGarbageCollector is a garbage collector that will delete nodes after
// a certain amount of time.
// It is used to delete ephemeral nodes ([types.Node.IsEphemeral]) that have disconnected and should be
// cleaned up.
type EphemeralGarbageCollector struct {
	mu sync.Mutex

	deleteFunc  func(types.NodeID)
	toBeDeleted map[types.NodeID]ephemeralTimer
	// gen is bumped for every scheduled deletion so a queued deletion that
	// was superseded by a Cancel or reschedule can be recognised and dropped.
	gen uint64

	deleteCh chan pendingDeletion
	cancelCh chan struct{}
}

// ephemeralTimer pairs a node's pending-deletion timer with a done channel
// used to reap its watcher goroutine on Cancel or reschedule, plus the
// generation identifying this particular scheduling. Without the done channel
// a stopped timer never fires and the goroutine leaks until Close.
type ephemeralTimer struct {
	timer *time.Timer
	done  chan struct{}
	gen   uint64
}

// pendingDeletion is the generation-stamped deletion a watcher enqueues when
// its timer fires. Start drops it if the node's current generation no longer
// matches, i.e. it was cancelled or rescheduled in the meantime.
type pendingDeletion struct {
	nodeID types.NodeID
	gen    uint64
}

// NewEphemeralGarbageCollector creates a new [EphemeralGarbageCollector], it takes
// a deleteFunc that will be called when a node is scheduled for deletion.
func NewEphemeralGarbageCollector(deleteFunc func(types.NodeID)) *EphemeralGarbageCollector {
	return &EphemeralGarbageCollector{
		toBeDeleted: make(map[types.NodeID]ephemeralTimer),
		deleteCh:    make(chan pendingDeletion, 10),
		cancelCh:    make(chan struct{}),
		deleteFunc:  deleteFunc,
	}
}

// Close stops the garbage collector.
func (e *EphemeralGarbageCollector) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Stop all timers
	for _, t := range e.toBeDeleted {
		t.timer.Stop()
	}

	// Close the cancel channel to signal all goroutines to exit
	close(e.cancelCh)
}

// Schedule schedules a node for deletion after the expiry duration.
// If the garbage collector is already closed, this is a no-op.
func (e *EphemeralGarbageCollector) Schedule(nodeID types.NodeID, expiry time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Don't schedule new timers if the garbage collector is already closed
	select {
	case <-e.cancelCh:
		// The cancel channel is closed, meaning the GC is shutting down
		// or already shut down, so we shouldn't schedule anything new
		return
	default:
		// Continue with scheduling
	}

	// If a timer already exists for this node, stop it and reap its
	// watcher goroutine before scheduling a fresh one.
	if old, exists := e.toBeDeleted[nodeID]; exists {
		old.timer.Stop()
		close(old.done)
	}

	e.gen++
	gen := e.gen
	timer := time.NewTimer(expiry)
	done := make(chan struct{})
	e.toBeDeleted[nodeID] = ephemeralTimer{timer: timer, done: done, gen: gen}
	// Start a goroutine to handle the timer completion
	go func() {
		select {
		case <-timer.C:
			// This is to handle the situation where the GC is shutting down and
			// we are trying to schedule a new node for deletion at the same time
			// i.e. We don't want to send to deleteCh if the GC is shutting down
			// So, we try to send to deleteCh, but also watch for cancelCh
			select {
			case e.deleteCh <- pendingDeletion{nodeID: nodeID, gen: gen}:
				// Successfully sent to deleteCh
			case <-e.cancelCh:
				// GC is shutting down, don't send to deleteCh
				return
			case <-done:
				// Cancelled or rescheduled before the send landed.
				return
			}
		case <-done:
			// Cancelled or rescheduled before the timer fired.
			return
		case <-e.cancelCh:
			// If the GC is closed, exit the goroutine
			return
		}
	}()
}

// Cancel cancels the deletion of a node.
func (e *EphemeralGarbageCollector) Cancel(nodeID types.NodeID) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if t, ok := e.toBeDeleted[nodeID]; ok {
		t.timer.Stop()
		close(t.done)
		delete(e.toBeDeleted, nodeID)
	}
}

// IsScheduled reports whether a deletion timer is currently armed for nodeID.
func (e *EphemeralGarbageCollector) IsScheduled(nodeID types.NodeID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, ok := e.toBeDeleted[nodeID]

	return ok
}

// Start starts the garbage collector.
func (e *EphemeralGarbageCollector) Start() {
	for {
		select {
		case <-e.cancelCh:
			return
		case pd := <-e.deleteCh:
			e.mu.Lock()

			entry, ok := e.toBeDeleted[pd.nodeID]
			if !ok || entry.gen != pd.gen {
				// Cancelled or rescheduled after this deletion was queued;
				// drop it so a reconnected node is not removed.
				e.mu.Unlock()

				continue
			}

			delete(e.toBeDeleted, pd.nodeID)
			e.mu.Unlock()

			go e.deleteFunc(pd.nodeID)
		}
	}
}

// firstOr returns the first non-empty option, or def if none is provided.
func firstOr(def string, opt []string) string {
	if len(opt) > 0 && opt[0] != "" {
		return opt[0]
	}

	return def
}

func (hsdb *HSDatabase) CreateNodeForTest(user *types.User, hostname ...string) *types.Node {
	if !testing.Testing() {
		panic("CreateNodeForTest can only be called during tests")
	}

	if user == nil {
		panic("CreateNodeForTest requires a valid user")
	}

	nodeName := firstOr(defaultTestNodePrefix, hostname)

	// Create a preauth key for the node
	pak, err := hsdb.CreatePreAuthKey(user.TypedID(), false, false, nil, nil)
	if err != nil {
		panic(fmt.Sprintf("failed to create preauth key for test node: %v", err))
	}

	pakID := pak.ID
	nodeKey := key.NewNode()
	machineKey := key.NewMachine()
	discoKey := key.NewDisco()

	node := &types.Node{
		MachineKey:     machineKey.Public(),
		NodeKey:        nodeKey.Public(),
		DiscoKey:       discoKey.Public(),
		Hostname:       nodeName,
		UserID:         &user.ID,
		RegisterMethod: util.RegisterMethodAuthKey,
		AuthKeyID:      &pakID,
		ApprovedAt:     new(time.Now().UTC()),
	}

	err = CreateNode(hsdb, node)
	if err != nil {
		panic(fmt.Sprintf("failed to create test node: %v", err))
	}

	return node
}

func (hsdb *HSDatabase) CreateRegisteredNodeForTest(user *types.User, hostname ...string) *types.Node {
	if !testing.Testing() {
		panic("CreateRegisteredNodeForTest can only be called during tests")
	}

	node := hsdb.CreateNodeForTest(user, hostname...)

	// Allocate IPs for the test node using the database's IP allocator
	// This is a simplified allocation for testing - in production this would use State.ipAlloc
	ipv4, ipv6, err := hsdb.allocateTestIPs(node.ID)
	if err != nil {
		panic(fmt.Sprintf("failed to allocate IPs for test node: %v", err))
	}

	var registeredNode *types.Node

	err = hsdb.Write(func(tx *Tx) error {
		var registerErr error

		registeredNode, registerErr = RegisterNodeForTest(tx, *node, ipv4, ipv6)

		return registerErr
	})
	if err != nil {
		panic(fmt.Sprintf("failed to register test node: %v", err))
	}

	return registeredNode
}

func (hsdb *HSDatabase) CreateNodesForTest(user *types.User, count int, hostnamePrefix ...string) []*types.Node {
	if !testing.Testing() {
		panic("CreateNodesForTest can only be called during tests")
	}

	if user == nil {
		panic("CreateNodesForTest requires a valid user")
	}

	prefix := firstOr(defaultTestNodePrefix, hostnamePrefix)

	nodes := make([]*types.Node, count)
	for i := range count {
		hostname := prefix + "-" + strconv.Itoa(i)
		nodes[i] = hsdb.CreateNodeForTest(user, hostname)
	}

	return nodes
}

func (hsdb *HSDatabase) CreateRegisteredNodesForTest(
	user *types.User,
	count int,
	hostnamePrefix ...string,
) []*types.Node {
	if !testing.Testing() {
		panic("CreateRegisteredNodesForTest can only be called during tests")
	}

	if user == nil {
		panic("CreateRegisteredNodesForTest requires a valid user")
	}

	prefix := firstOr(defaultTestNodePrefix, hostnamePrefix)

	nodes := make([]*types.Node, count)
	for i := range count {
		hostname := prefix + "-" + strconv.Itoa(i)
		nodes[i] = hsdb.CreateRegisteredNodeForTest(user, hostname)
	}

	return nodes
}

// allocateTestIPs allocates sequential test IPs for nodes during testing.
func (hsdb *HSDatabase) allocateTestIPs(nodeID types.NodeID) (*netip.Addr, *netip.Addr, error) {
	if !testing.Testing() {
		panic("allocateTestIPs can only be called during tests")
	}

	// Use simple sequential allocation for tests
	// IPv4: 100.64.x.y (where x = nodeID/256, y = nodeID%256)
	// IPv6: fd7a:115c:a1e0::x:y (where x = high byte, y = low byte)
	// This supports up to 65535 nodes
	const (
		maxTestNodes    = 65535
		ipv4ByteDivisor = 256
	)

	if nodeID > maxTestNodes {
		return nil, nil, ErrCouldNotAllocateIP
	}

	// Split nodeID into high and low bytes for IPv4 (100.64.high.low)
	highByte := byte(nodeID / ipv4ByteDivisor)
	lowByte := byte(nodeID % ipv4ByteDivisor)
	ipv4 := netip.AddrFrom4([4]byte{100, 64, highByte, lowByte})

	// For IPv6, use the last two bytes of the address (fd7a:115c:a1e0::high:low)
	ipv6 := netip.AddrFrom16([16]byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0, 0, 0, 0, 0, 0, 0, 0, highByte, lowByte})

	return &ipv4, &ipv6, nil
}
