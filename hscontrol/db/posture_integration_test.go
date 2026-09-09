package db

import (
	"slices"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostureIntegrationCRUD(t *testing.T) {
	t.Parallel()

	db := dbForTest(t)

	// Create
	created, err := db.CreatePostureIntegration(types.PostureIntegration{
		Provider: types.PostureProviderFalcon,
		Name:     "falcon-prod",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			BaseURL:      "https://api.crowdstrike.com",
		},
	})
	require.NoError(t, err)
	assert.NotZero(t, created.ID)
	assert.Equal(t, "falcon-prod", created.Name)
	assert.Equal(t, types.PostureProviderFalcon, created.Provider)
	assert.True(t, created.Enabled)
	assert.Equal(t, "client-id", created.Config.ClientID)
	assert.False(t, created.CreatedAt.IsZero())
	assert.False(t, created.UpdatedAt.IsZero())

	// Get
	got, err := getPostureIntegration(db, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "falcon-prod", got.Name)
	assert.Equal(t, "client-secret", got.Config.ClientSecret)

	// List
	list, err := db.ListPostureIntegrations()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, created.ID, list[0].ID)

	// Update
	created.Name = "falcon-renamed"
	created.Enabled = false
	created.Config.ClientID = "client-id-2"

	updated, err := db.UpdatePostureIntegration(created)
	require.NoError(t, err)
	assert.Equal(t, "falcon-renamed", updated.Name)
	assert.False(t, updated.Enabled)
	assert.Equal(t, "client-id-2", updated.Config.ClientID)

	// Update non-existent returns not found
	nonExistent := created
	nonExistent.ID = types.PostureIntegrationID(999999)

	_, err = db.UpdatePostureIntegration(nonExistent)
	require.ErrorIs(t, err, types.ErrPostureIntegrationNotFound)

	// Delete
	err = db.DeletePostureIntegration(created.ID)
	require.NoError(t, err)

	_, err = getPostureIntegration(db, created.ID)
	require.ErrorIs(t, err, types.ErrPostureIntegrationNotFound)

	// Delete non-existent returns not found
	err = db.DeletePostureIntegration(created.ID)
	require.ErrorIs(t, err, types.ErrPostureIntegrationNotFound)
}

func TestPostureIntegrationNameUniqueness(t *testing.T) {
	t.Parallel()

	db := dbForTest(t)

	_, err := db.CreatePostureIntegration(types.PostureIntegration{
		Provider: types.PostureProviderFalcon,
		Name:     "unique-name",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			ClientID:     "cid",
			ClientSecret: "sec",
		},
	})
	require.NoError(t, err)

	// Creating with same name returns ErrPostureIntegrationNameTaken
	_, err = db.CreatePostureIntegration(types.PostureIntegration{
		Provider: types.PostureProviderSentinelOne,
		Name:     "unique-name",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			APIToken: "token",
			BaseURL:  "https://s1.example.com",
		},
	})
	require.ErrorIs(t, err, types.ErrPostureIntegrationNameTaken)

	// Creating second integration with different name succeeds
	second, err := db.CreatePostureIntegration(types.PostureIntegration{
		Provider: types.PostureProviderSentinelOne,
		Name:     "other-name",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			APIToken: "token",
			BaseURL:  "https://s1.example.com",
		},
	})
	require.NoError(t, err)

	// Updating second to same name as first returns ErrPostureIntegrationNameTaken
	second.Name = "unique-name"

	_, err = db.UpdatePostureIntegration(second)
	require.ErrorIs(t, err, types.ErrPostureIntegrationNameTaken)
}

func TestRecordPostureIntegrationSync(t *testing.T) {
	t.Parallel()

	db := dbForTest(t)

	created, err := db.CreatePostureIntegration(types.PostureIntegration{
		Provider: types.PostureProviderFalcon,
		Name:     "sync-test",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			ClientID:     "cid",
			ClientSecret: "sec",
		},
	})
	require.NoError(t, err)

	syncTime := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

	err = db.RecordPostureIntegrationSync(created.ID, syncTime, "token expired", 42)
	require.NoError(t, err)

	got, err := getPostureIntegration(db, created.ID)
	require.NoError(t, err)
	assert.Equal(t, syncTime, got.LastSyncAt)
	assert.Equal(t, "token expired", got.LastError)
	assert.Equal(t, 42, got.LastMatched)
}

func TestReplacePrefixedNodeAttributes(t *testing.T) {
	t.Parallel()

	db := dbForTest(t)

	user := db.CreateUserForTest("test-user")
	node := db.CreateNodeForTest(user, "test-node")

	// Set a custom: attribute
	err := SetNodeAttribute(db, node.ID, types.NodeAttribute{
		Key:   "custom:environment",
		Value: types.AttributeValue{Kind: types.AttributeString, String: "staging"},
	})
	require.NoError(t, err)

	// Replace falcon attributes
	err = db.ReplacePrefixedNodeAttributes("falcon:", map[types.NodeID][]types.NodeAttribute{
		node.ID: {
			{
				Key:   "falcon:ztaScore",
				Value: types.AttributeValue{Kind: types.AttributeNumber, Number: 85},
			},
			{
				Key:   "falcon:aid",
				Value: types.AttributeValue{Kind: types.AttributeString, String: "aid-123"},
			},
		},
	})
	require.NoError(t, err)

	// Verify node has custom: and both falcon: attributes
	n, err := db.GetNodeByID(node.ID)
	require.NoError(t, err)
	require.Len(t, n.Attributes, 3)

	attrMap := make(map[string]types.AttributeValue)
	for _, a := range n.Attributes {
		attrMap[a.Key] = a.Value
	}

	assert.Equal(t, "staging", attrMap["custom:environment"].String)
	assert.InDelta(t, 85.0, attrMap["falcon:ztaScore"].Number, 0.001)
	assert.Equal(t, "aid-123", attrMap["falcon:aid"].String)

	// Replace falcon attributes again: falcon:aid removed, only falcon:ztaScore kept with new value
	err = db.ReplacePrefixedNodeAttributes("falcon:", map[types.NodeID][]types.NodeAttribute{
		node.ID: {
			{
				Key:   "falcon:ztaScore",
				Value: types.AttributeValue{Kind: types.AttributeNumber, Number: 95},
			},
		},
	})
	require.NoError(t, err)

	n, err = db.GetNodeByID(node.ID)
	require.NoError(t, err)
	require.Len(t, n.Attributes, 2)

	attrMap = make(map[string]types.AttributeValue)
	for _, a := range n.Attributes {
		attrMap[a.Key] = a.Value
	}

	assert.Equal(t, "staging", attrMap["custom:environment"].String)
	assert.InDelta(t, 95.0, attrMap["falcon:ztaScore"].Number, 0.001)
	assert.NotContains(t, attrMap, "falcon:aid")

	// Replace with empty slice removes all falcon: attributes while leaving custom:
	err = db.ReplacePrefixedNodeAttributes("falcon:", map[types.NodeID][]types.NodeAttribute{
		node.ID: nil,
	})
	require.NoError(t, err)

	n, err = db.GetNodeByID(node.ID)
	require.NoError(t, err)
	require.Len(t, n.Attributes, 1)
	assert.Equal(t, "custom:environment", n.Attributes[0].Key)
	assert.Equal(t, "staging", n.Attributes[0].Value.String)
}

func TestDeletePrefixedNodeAttributes(t *testing.T) {
	t.Parallel()

	db := dbForTest(t)

	user := db.CreateUserForTest("test-user-del")
	node1 := db.CreateNodeForTest(user, "node-1")
	node2 := db.CreateNodeForTest(user, "node-2")
	node3 := db.CreateNodeForTest(user, "node-3")

	// Set falcon: on node1 and node2; custom: on node2; sentinelOne: on node3
	err := SetNodeAttribute(db, node1.ID, types.NodeAttribute{
		Key:   "falcon:ztaScore",
		Value: types.AttributeValue{Kind: types.AttributeNumber, Number: 70},
	})
	require.NoError(t, err)

	err = SetNodeAttribute(db, node2.ID, types.NodeAttribute{
		Key:   "falcon:ztaScore",
		Value: types.AttributeValue{Kind: types.AttributeNumber, Number: 80},
	})
	require.NoError(t, err)

	err = SetNodeAttribute(db, node2.ID, types.NodeAttribute{
		Key:   "custom:owner",
		Value: types.AttributeValue{Kind: types.AttributeString, String: "bob"},
	})
	require.NoError(t, err)

	err = SetNodeAttribute(db, node3.ID, types.NodeAttribute{
		Key:   "sentinelOne:infected",
		Value: types.AttributeValue{Kind: types.AttributeBool, Bool: false},
	})
	require.NoError(t, err)

	// Delete falcon: attributes
	deletedIDs, err := db.DeletePrefixedNodeAttributes("falcon:")
	require.NoError(t, err)

	// Returned IDs should be node1 and node2
	assert.Len(t, deletedIDs, 2)
	assert.True(t, slices.Contains(deletedIDs, node1.ID))
	assert.True(t, slices.Contains(deletedIDs, node2.ID))

	// Node 1 has no attributes left
	n1, err := db.GetNodeByID(node1.ID)
	require.NoError(t, err)
	assert.Empty(t, n1.Attributes)

	// Node 2 still has custom:owner
	n2, err := db.GetNodeByID(node2.ID)
	require.NoError(t, err)
	require.Len(t, n2.Attributes, 1)
	assert.Equal(t, "custom:owner", n2.Attributes[0].Key)

	// Node 3 still has sentinelOne:infected
	n3, err := db.GetNodeByID(node3.ID)
	require.NoError(t, err)
	require.Len(t, n3.Attributes, 1)
	assert.Equal(t, "sentinelOne:infected", n3.Attributes[0].Key)

	// Calling delete again when none exist returns nil, nil
	deletedIDsAgain, err := db.DeletePrefixedNodeAttributes("falcon:")
	require.NoError(t, err)
	assert.Nil(t, deletedIDsAgain)
}
