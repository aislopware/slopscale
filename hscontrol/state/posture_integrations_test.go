package state

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/posture/integration"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePostureIntegrationClient struct {
	attrs map[string]integration.Attributes
	err   error
}

func (f *fakePostureIntegrationClient) Lookup(
	_ context.Context, _ []string,
) (map[string]integration.Attributes, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.attrs, nil
}

func (f *fakePostureIntegrationClient) Check(_ context.Context) error {
	return f.err
}

func TestPostureIntegrations(t *testing.T) {
	// No t.Parallel() because postureIntegrationClient is a package-global variable.
	s := newRoleTestState(t)

	user := s.CreateUserForTest("alice")
	node := s.CreateNodeForTest(user, "alice-laptop")
	s.PutNodeInStoreForTest(*node)

	_, _, err := s.setPosture(node.ID, types.PostureIdentity{
		SerialNumbers: []string{"SERIAL1"},
	})
	require.NoError(t, err)

	fake := &fakePostureIntegrationClient{
		attrs: map[string]integration.Attributes{
			"SERIAL1": {"ztaScore": 87},
		},
	}

	oldClient := postureIntegrationClient
	postureIntegrationClient = func(_ types.PostureIntegration) (integration.Client, error) {
		return fake, nil
	}

	t.Cleanup(func() {
		postureIntegrationClient = oldClient
	})

	// CreatePostureIntegration stores and syncs at once
	created, _, err := s.CreatePostureIntegration(t.Context(), types.PostureIntegration{
		Provider: types.PostureProviderFalcon,
		Name:     "falcon-primary",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, created.LastMatched)

	// Assert after CreatePostureIntegration the node has attribute falcon:ztaScore = 87
	n, ok := s.GetNodeByID(node.ID)
	require.True(t, ok)

	var score *float64

	for _, a := range n.Attributes().All() {
		if a.Key == "falcon:ztaScore" {
			val := a.Value.Number
			score = &val
		}
	}

	require.NotNil(t, score)
	assert.InDelta(t, 87.0, *score, 0.001)

	// A second enabled falcon integration -> ErrPostureProviderEnabled
	_, _, err = s.CreatePostureIntegration(t.Context(), types.PostureIntegration{
		Provider: types.PostureProviderFalcon,
		Name:     "falcon-secondary",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			ClientID:     "client-id-2",
			ClientSecret: "client-secret-2",
		},
	})
	require.ErrorIs(t, err, ErrPostureProviderEnabled)

	// Update with empty secret keeps stored secret
	updated, _, err := s.UpdatePostureIntegration(t.Context(), types.PostureIntegration{
		ID:       created.ID,
		Provider: types.PostureProviderFalcon,
		Name:     "falcon-renamed",
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			ClientID:     "client-id-updated",
			ClientSecret: "",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "falcon-renamed", updated.Name)
	assert.Equal(t, "client-secret", updated.Config.ClientSecret)

	// A failing client records LastError and keeps the previous attributes
	fake.err = errors.New("crowdstrike api offline")

	synced, _, err := s.SyncPostureIntegration(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "crowdstrike api offline", synced.LastError)

	n, ok = s.GetNodeByID(node.ID)
	require.True(t, ok)

	score = nil

	for _, a := range n.Attributes().All() {
		if a.Key == "falcon:ztaScore" {
			val := a.Value.Number
			score = &val
		}
	}

	require.NotNil(t, score)
	assert.InDelta(t, 87.0, *score, 0.001)

	// Delete removes the attributes
	_, err = s.DeletePostureIntegration(created.ID)
	require.NoError(t, err)

	n, ok = s.GetNodeByID(node.ID)
	require.True(t, ok)

	for _, a := range n.Attributes().All() {
		assert.False(t, strings.HasPrefix(a.Key, "falcon:"))
	}
}

func TestCheckPostureIntegration(t *testing.T) {
	// No t.Parallel() because postureIntegrationClient is a package-global variable.
	s := newRoleTestState(t)

	fake := &fakePostureIntegrationClient{}

	oldClient := postureIntegrationClient
	postureIntegrationClient = func(_ types.PostureIntegration) (integration.Client, error) {
		return fake, nil
	}

	t.Cleanup(func() {
		postureIntegrationClient = oldClient
	})

	// Check succeeds
	err := s.CheckPostureIntegration(t.Context(), types.PostureIntegration{
		Provider: types.PostureProviderFalcon,
		Name:     "falcon-check",
		Config: types.PostureIntegrationConfig{
			ClientID:     "cid",
			ClientSecret: "csec",
		},
	})
	require.NoError(t, err)

	// Check fails on error
	fake.err = errors.New("bad credentials")

	err = s.CheckPostureIntegration(t.Context(), types.PostureIntegration{
		Provider: types.PostureProviderFalcon,
		Name:     "falcon-check",
		Config: types.PostureIntegrationConfig{
			ClientID:     "cid",
			ClientSecret: "csec",
		},
	})
	require.ErrorContains(t, err, "bad credentials")
}
