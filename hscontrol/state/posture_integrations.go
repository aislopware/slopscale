package state

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/posture/integration"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
)

// Posture integrations ask a device management or endpoint security
// service about every machine with a serial number and write the answer
// as prefixed posture attributes (falcon:ztaScore, intune:complianceState).
// Each provider owns its prefix: one enabled integration per provider,
// and a sync replaces everything under the prefix, so a machine the
// provider stopped knowing loses the attributes. See
// docs/ref/device-trust.md.

// PostureIntegrationSyncInterval is how often the scheduled worker asks
// every enabled provider. Tailscale syncs about as often.
const PostureIntegrationSyncInterval = 15 * time.Minute

// postureIntegrationTimeout bounds one provider's sync or check;
// postureProviderRequestTimeout one request within it.
const (
	postureIntegrationTimeout     = 2 * time.Minute
	postureProviderRequestTimeout = 30 * time.Second
)

// ErrPostureProviderEnabled is a second enabled integration for a
// provider: they would fight over the prefix.
var ErrPostureProviderEnabled = errors.New("an enabled integration for that provider exists")

// postureIntegrationClient builds the provider client; tests replace it.
var postureIntegrationClient = func(i types.PostureIntegration) (integration.Client, error) {
	return integration.New(i, &http.Client{Timeout: postureProviderRequestTimeout, Transport: egress.Transport()})
}

// PostureIntegrations returns every integration in name order.
func (s *State) PostureIntegrations() []types.PostureIntegration {
	list := s.postureIntegrations.Load()
	if list == nil {
		return nil
	}

	return slices.Clone(*list)
}

// GetPostureIntegration returns one integration.
func (s *State) GetPostureIntegration(id types.PostureIntegrationID) (types.PostureIntegration, error) {
	for _, i := range s.PostureIntegrations() {
		if i.ID == id {
			return i, nil
		}
	}

	return types.PostureIntegration{}, types.ErrPostureIntegrationNotFound
}

// loadPostureIntegrations reads the integrations from the database into
// the cache; at start and after every mutation.
func (s *State) loadPostureIntegrations() error {
	list, err := s.db.ListPostureIntegrations()
	if err != nil {
		return fmt.Errorf("loading posture integrations: %w", err)
	}

	s.postureIntegrations.Store(&list)

	return nil
}

// validatePostureIntegration normalises the definition and refuses a
// base URL on the server's own network and a second enabled integration
// for the provider.
func (s *State) validatePostureIntegration(i *types.PostureIntegration) error {
	err := i.Normalize()
	if err != nil {
		return err
	}

	if i.Config.BaseURL != "" {
		u, err := url.Parse(i.Config.BaseURL)
		if err != nil {
			return fmt.Errorf("%w: %w", types.ErrPostureIntegrationConfig, err)
		}

		err = egress.Default().CheckHost(u.Host)
		if err != nil {
			return fmt.Errorf("base URL points at a %w", err)
		}
	}

	if !i.Enabled {
		return nil
	}

	for _, other := range s.PostureIntegrations() {
		if other.ID != i.ID && other.Enabled && other.Provider == i.Provider {
			return fmt.Errorf("%w: %s", ErrPostureProviderEnabled, other.Name)
		}
	}

	return nil
}

// CreatePostureIntegration stores an integration and, when enabled, syncs
// it at once so the attributes exist before the operator writes a
// posture against them.
func (s *State) CreatePostureIntegration(
	ctx context.Context, i types.PostureIntegration,
) (types.PostureIntegration, change.Change, error) {
	err := s.validatePostureIntegration(&i)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	created, err := s.db.CreatePostureIntegration(i)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	err = s.loadPostureIntegrations()
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	log.Info().Str("integration.name", created.Name).Str("integration.provider", string(created.Provider)).
		Msg("posture integration created")

	return s.syncAfterChange(ctx, created)
}

// UpdatePostureIntegration replaces an integration's definition. A secret
// left empty keeps the stored one, so the console can edit the rest
// without asking for the credential again.
func (s *State) UpdatePostureIntegration(
	ctx context.Context, i types.PostureIntegration,
) (types.PostureIntegration, change.Change, error) {
	existing, err := s.GetPostureIntegration(i.ID)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	if i.Config.ClientSecret == "" {
		i.Config.ClientSecret = existing.Config.ClientSecret
	}

	if i.Config.APIToken == "" {
		i.Config.APIToken = existing.Config.APIToken
	}

	err = s.validatePostureIntegration(&i)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	updated, err := s.db.UpdatePostureIntegration(i)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	err = s.loadPostureIntegrations()
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	// A provider change or a disable leaves the old prefix's attributes
	// behind; drop them so the policy stops seeing them.
	var c change.Change

	if existing.Provider != updated.Provider || (existing.Enabled && !updated.Enabled) {
		c, err = s.dropPostureAttributes(existing.Provider)
		if err != nil {
			return types.PostureIntegration{}, change.Change{}, err
		}
	}

	updated, sc, err := s.syncAfterChange(ctx, updated)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	return updated, c.Merge(sc), nil
}

// syncAfterChange syncs an enabled integration right away; a provider
// failure is recorded on the integration, not returned, since the
// definition is saved either way.
func (s *State) syncAfterChange(
	ctx context.Context, i types.PostureIntegration,
) (types.PostureIntegration, change.Change, error) {
	if !i.Enabled {
		return i, change.Change{}, nil
	}

	synced, c, err := s.SyncPostureIntegration(ctx, i.ID)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	return synced, c, nil
}

// DeletePostureIntegration removes an integration and the attributes its
// provider wrote.
func (s *State) DeletePostureIntegration(id types.PostureIntegrationID) (change.Change, error) {
	existing, err := s.GetPostureIntegration(id)
	if err != nil {
		return change.Change{}, err
	}

	err = s.db.DeletePostureIntegration(id)
	if err != nil {
		return change.Change{}, err
	}

	err = s.loadPostureIntegrations()
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Str("integration.name", existing.Name).Msg("posture integration deleted")

	return s.dropPostureAttributes(existing.Provider)
}

// dropPostureAttributes removes every node's attributes under the
// provider's prefix and recomputes the policy.
func (s *State) dropPostureAttributes(provider types.PostureProvider) (change.Change, error) {
	prefix := provider.Prefix()

	ids, err := s.db.DeletePrefixedNodeAttributes(prefix)
	if err != nil {
		return change.Change{}, err
	}

	if len(ids) == 0 {
		return change.Change{}, nil
	}

	updates := make(map[types.NodeID]UpdateNodeFunc, len(ids))
	for _, id := range ids {
		updates[id] = func(node *types.Node) {
			node.Attributes = slices.DeleteFunc(node.Attributes, func(a types.NodeAttribute) bool {
				return strings.HasPrefix(a.Key, prefix)
			})
		}
	}

	s.nodeStore.UpdateNodes(updates)

	return s.updatePolicyManagerNodes()
}

// CheckPostureIntegration verifies that the credentials reach the
// provider without storing anything. An empty secret on a check against
// a stored integration uses the stored one.
func (s *State) CheckPostureIntegration(ctx context.Context, i types.PostureIntegration) error {
	if i.ID != 0 {
		existing, err := s.GetPostureIntegration(i.ID)
		if err == nil {
			if i.Config.ClientSecret == "" {
				i.Config.ClientSecret = existing.Config.ClientSecret
			}

			if i.Config.APIToken == "" {
				i.Config.APIToken = existing.Config.APIToken
			}
		}
	}

	// Enabled is irrelevant to a check; clear it so the one-per-provider
	// rule does not get in the way.
	i.Enabled = false

	err := s.validatePostureIntegration(&i)
	if err != nil {
		return err
	}

	client, err := postureIntegrationClient(i)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, postureIntegrationTimeout)
	defer cancel()

	return client.Check(ctx)
}

// SyncPostureIntegrations asks every enabled provider; the scheduled
// worker calls it. One provider's failure is recorded on its integration
// and the others still run.
func (s *State) SyncPostureIntegrations(ctx context.Context) change.Change {
	var c change.Change

	for _, i := range s.PostureIntegrations() {
		if !i.Enabled {
			continue
		}

		_, sc, err := s.SyncPostureIntegration(ctx, i.ID)
		if err != nil {
			log.Error().Err(err).Str("integration.name", i.Name).Msg("syncing posture integration")

			continue
		}

		c = c.Merge(sc)
	}

	return c
}

// SyncPostureIntegration asks one provider about every machine with a
// serial number and replaces the attributes under its prefix. The
// provider's answer, or failure, is recorded on the integration; only a
// database error is returned.
func (s *State) SyncPostureIntegration(
	ctx context.Context, id types.PostureIntegrationID,
) (types.PostureIntegration, change.Change, error) {
	s.postureSyncMu.Lock()
	defer s.postureSyncMu.Unlock()

	i, err := s.GetPostureIntegration(id)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	byNode, matched, syncErr := s.lookupPostureIntegration(ctx, i)

	now := time.Now().UTC()

	var errText string
	if syncErr != nil {
		errText = syncErr.Error()
	}

	err = s.db.RecordPostureIntegrationSync(id, now, errText, matched)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	err = s.loadPostureIntegrations()
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	synced, err := s.GetPostureIntegration(id)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	if syncErr != nil {
		// The previous attributes stay: a provider outage should not
		// drop every machine out of the postures that need it.
		log.Warn().Err(syncErr).Str("integration.name", i.Name).Msg("posture integration sync failed")

		return synced, change.Change{}, nil
	}

	c, err := s.applyPostureAttributes(i.Provider.Prefix(), byNode)
	if err != nil {
		return types.PostureIntegration{}, change.Change{}, err
	}

	log.Info().Str("integration.name", i.Name).Int("matched", matched).Msg("posture integration synced")

	return synced, c, nil
}

// lookupPostureIntegration asks the provider about every serial the
// nodes report and returns each node's attributes, empty for a node the
// provider does not know, with how many nodes it knew.
func (s *State) lookupPostureIntegration(
	ctx context.Context, i types.PostureIntegration,
) (map[types.NodeID][]types.NodeAttribute, int, error) {
	client, err := postureIntegrationClient(i)
	if err != nil {
		return nil, 0, err
	}

	serialsOf := make(map[types.NodeID][]string)
	serialSet := make(map[string]struct{})

	for _, node := range s.ListNodes().All() {
		if !node.Posture().Valid() {
			continue
		}

		for _, serial := range node.Posture().SerialNumbers().All() {
			if serial == "" {
				continue
			}

			serialsOf[node.ID()] = append(serialsOf[node.ID()], serial)
			serialSet[serial] = struct{}{}
		}
	}

	byNode := make(map[types.NodeID][]types.NodeAttribute, len(serialsOf))

	if len(serialSet) == 0 {
		return byNode, 0, nil
	}

	serials := make([]string, 0, len(serialSet))
	for serial := range serialSet {
		serials = append(serials, serial)
	}

	slices.Sort(serials)

	ctx, cancel := context.WithTimeout(ctx, postureIntegrationTimeout)
	defer cancel()

	found, err := client.Lookup(ctx, serials)
	if err != nil {
		return nil, 0, err
	}

	matched := 0
	prefix := i.Provider.Prefix()

	for nodeID, nodeSerials := range serialsOf {
		byNode[nodeID] = nil

		for _, serial := range nodeSerials {
			attrs, ok := found[serial]
			if !ok {
				continue
			}

			byNode[nodeID] = prefixedAttributes(prefix, attrs)
			matched++

			break
		}
	}

	return byNode, matched, nil
}

// prefixedAttributes turns a provider's answer into stored attributes,
// in key order so a sync writes them the same way every time.
func prefixedAttributes(prefix string, attrs integration.Attributes) []types.NodeAttribute {
	out := make([]types.NodeAttribute, 0, len(attrs))

	for name, value := range attrs {
		v, err := types.AttributeValueOf(value)
		if err != nil {
			continue
		}

		out = append(out, types.NodeAttribute{Key: prefix + name, Value: v})
	}

	types.SortAttributes(out)

	return out
}

// applyPostureAttributes stores each node's attributes under the prefix
// and recomputes the policy once. Nodes with no serial keep whatever the
// prefix had, which is nothing, since a provider only knows serials.
func (s *State) applyPostureAttributes(
	prefix string, byNode map[types.NodeID][]types.NodeAttribute,
) (change.Change, error) {
	if len(byNode) == 0 {
		return change.Change{}, nil
	}

	err := s.db.ReplacePrefixedNodeAttributes(prefix, byNode)
	if err != nil {
		return change.Change{}, fmt.Errorf("storing %s attributes: %w", prefix, err)
	}

	updates := make(map[types.NodeID]UpdateNodeFunc, len(byNode))
	for nodeID, attrs := range byNode {
		updates[nodeID] = func(node *types.Node) {
			node.Attributes = slices.DeleteFunc(node.Attributes, func(a types.NodeAttribute) bool {
				return strings.HasPrefix(a.Key, prefix)
			})
			node.Attributes = append(node.Attributes, attrs...)
			types.SortAttributes(node.Attributes)
		}
	}

	s.nodeStore.UpdateNodes(updates)

	return s.updatePolicyManagerNodes()
}
