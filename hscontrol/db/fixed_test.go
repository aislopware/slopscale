package db

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedCase pairs a fixed statement and the arguments a call site passes
// with the jet statement it replaces, built with the same values.
type fixedCase struct {
	name  string
	fixed *fixedSQL
	args  []any
	jet   statement
}

func fixedCases(t *testing.T) []fixedCase {
	t.Helper()

	now := time.Now()
	ipv4 := "100.64.0.1"

	var user uint = 4

	row := nodeRow{
		ID:             7,
		MachineKey:     "mkey",
		NodeKey:        "nkey",
		Endpoints:      "[]",
		HostInfo:       "{}",
		Ipv4:           &ipv4,
		Hostname:       "host",
		UserID:         &user,
		RegisterMethod: "cli",
		Tags:           "null",
		LastSeen:       &now,
		ApprovedRoutes: "null",
		UpdatedAt:      now,
	}

	static := []fixedCase{
		{
			name:  "node by id",
			fixed: nodeByID,
			args:  []any{uint64(7), limitOne},
			jet:   selectNodes().WHERE(table.Nodes.ID.EQ(jet.Uint64(7))).LIMIT(1),
		},
		{
			name:  "node by node key",
			fixed: nodeByNodeKey,
			args:  []any{"nkey", limitOne},
			jet:   selectNodes().WHERE(table.Nodes.NodeKey.EQ(jet.String("nkey"))).LIMIT(1),
		},
		{name: "all nodes", fixed: allNodes, jet: selectNodes()},
		{
			name:  "peers of node",
			fixed: peersOfNode,
			args:  []any{uint64(7)},
			jet:   selectNodes().WHERE(table.Nodes.ID.NOT_EQ(jet.Uint64(7))),
		},
		{
			name:  "last seen",
			fixed: nodeLastSeen,
			args:  []any{now, now, uint64(7)},
			jet: table.Nodes.UPDATE(table.Nodes.LastSeen, table.Nodes.UpdatedAt).
				SET(now, now).WHERE(table.Nodes.ID.EQ(jet.Uint64(7))),
		},
		{
			name:  "user by id",
			fixed: userByID,
			args:  []any{uint64(4), limitOne},
			jet:   selectUser(table.Users.ID.EQ(jet.Uint64(4))),
		},
		{
			name:  "user by provider identifier",
			fixed: userByProviderIdentifier,
			args:  []any{"oidc/1", limitOne},
			jet:   selectUser(table.Users.ProviderIdentifier.EQ(jet.String("oidc/1"))),
		},
		{
			name:  "api key by prefix",
			fixed: apiKeyByPrefix,
			args:  []any{"abc", limitOne},
			jet:   selectAPIKey(table.APIKeys.Prefix.EQ(jet.String("abc"))),
		},
		{
			name:  "pre-auth key by key",
			fixed: preAuthKeyByKey,
			args:  []any{"k", limitOne},
			jet:   selectPreAuthKeys().WHERE(table.PreAuthKeys.Key.EQ(jet.String("k"))).LIMIT(1),
		},
		{
			name:  "pre-auth key by prefix",
			fixed: preAuthKeyByPrefix,
			args:  []any{"p", limitOne},
			jet:   selectPreAuthKeys().WHERE(table.PreAuthKeys.Prefix.EQ(jet.String("p"))).LIMIT(1),
		},
		{
			name:  "shares of node",
			fixed: sharesOfNode,
			args:  []any{uint64(7)},
			jet:   selectNodeShares().WHERE(table.NodeShares.NodeID.EQ(jet.Uint64(7))),
		},
		{name: "all shares", fixed: allShares, jet: selectNodeShares()},
		{
			name:  "attributes of node",
			fixed: attributesOfNode,
			args:  []any{uint64(7)},
			jet:   selectNodeAttributes().WHERE(table.NodeAttributes.NodeID.EQ(jet.Uint64(7))),
		},
		{name: "all attributes", fixed: allAttributes, jet: selectNodeAttributes()},
	}

	return slices.Concat(static, nodeUpdateCases(&row))
}

// nodeUpdateCases covers the four shapes of [UpdateNode].
func nodeUpdateCases(row *nodeRow) []fixedCase {
	updates := []NodeUpdate{{}, {Expiry: true}, {AuthKey: true}, {Expiry: true, AuthKey: true}}
	cases := make([]fixedCase, 0, len(updates))

	for _, update := range updates {
		cases = append(cases, fixedCase{
			name:  fmt.Sprintf("update node expiry=%t authkey=%t", update.Expiry, update.AuthKey),
			fixed: nodeUpdates[boolIndex(update.Expiry)][boolIndex(update.AuthKey)],
			args:  row.updateArgs(update),
			jet: table.Nodes.UPDATE(nodeUpdateColumns(update)).MODEL(*row).
				WHERE(table.Nodes.ID.EQ(jet.Uint64(row.ID))),
		})
	}

	return cases
}

// TestFixedStatementsMatchJet pins every fixed statement to the jet
// rendering it replaces: same text and, with the call site's arguments,
// the same bound values.
func TestFixedStatementsMatchJet(t *testing.T) {
	t.Parallel()

	for _, tc := range fixedCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wantQuery, wantArgs := tc.jet.Sql()
			wantQuery = strings.TrimSpace(wantQuery)

			gotQuery, nargs := tc.fixed.text(dialectSQLite)
			assert.Equal(t, wantQuery, gotQuery)
			assert.Equal(t, len(wantArgs), nargs)
			assert.Equal(t, wantArgs, tc.args)

			pgQuery, _ := tc.fixed.text(dialectPostgres)
			assert.Equal(t, postgresPlaceholders(wantQuery), pgQuery)
		})
	}
}

func TestFixedStatementRejectsArgumentCount(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	err = db.ex.queryFixed(nodeByID, &nodeRecord{}, uint64(1))
	require.ErrorIs(t, err, errFixedSQLArgs)
}
