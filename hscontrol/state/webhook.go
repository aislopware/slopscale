package state

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"tailscale.com/util/rands"
)

// webhookSecretPrefix marks a webhook secret so it is recognisable in a
// receiver's config; the rest is random.
const (
	webhookSecretPrefix = "hswh-"
	webhookSecretLength = 48
)

// newWebhookSecret mints a signing secret.
func newWebhookSecret() string {
	return webhookSecretPrefix + rands.HexString(webhookSecretLength)
}

// tailnetName is what webhook events call the tailnet: the base domain,
// or the server's host when MagicDNS has none.
func tailnetName(cfg *types.Config) string {
	if cfg == nil {
		return ""
	}

	return cmp.Or(cfg.BaseDomain, cfg.Domain())
}

// loadWebhooks reads the endpoints into the dispatcher.
func (s *State) loadWebhooks() error {
	hooks, err := s.db.ListWebhooks()
	if err != nil {
		return err
	}

	s.webhooks.Reload(hooks)

	return nil
}

// ListWebhooks returns every webhook endpoint, secrets included; the API
// layer decides what to show.
func (s *State) ListWebhooks() ([]types.Webhook, error) {
	return s.db.ListWebhooks()
}

// GetWebhook returns one endpoint.
func (s *State) GetWebhook(id types.WebhookID) (types.Webhook, error) {
	return s.db.GetWebhook(id)
}

// CreateWebhook stores an endpoint with a fresh secret and starts
// delivering to it. The returned record carries the secret; it is the
// only time it is shown.
func (s *State) CreateWebhook(w types.Webhook) (types.Webhook, error) {
	w.URL = strings.TrimSpace(w.URL)
	w.Secret = newWebhookSecret()

	err := types.ValidateWebhook(w)
	if err != nil {
		return types.Webhook{}, err
	}

	created, err := s.db.CreateWebhook(w)
	if err != nil {
		return types.Webhook{}, err
	}

	return created, s.loadWebhooks()
}

// UpdateWebhook replaces the URL, description, provider and
// subscriptions of an endpoint; the secret stays.
func (s *State) UpdateWebhook(w types.Webhook) (types.Webhook, error) {
	current, err := s.db.GetWebhook(w.ID)
	if err != nil {
		return types.Webhook{}, err
	}

	w.URL = strings.TrimSpace(w.URL)
	w.Secret = current.Secret
	w.CreatedBy = current.CreatedBy

	err = types.ValidateWebhook(w)
	if err != nil {
		return types.Webhook{}, err
	}

	updated, err := s.db.UpdateWebhook(w)
	if err != nil {
		return types.Webhook{}, err
	}

	return updated, s.loadWebhooks()
}

// RotateWebhookSecret replaces the endpoint's secret and returns the
// record with the new one.
func (s *State) RotateWebhookSecret(id types.WebhookID) (types.Webhook, error) {
	current, err := s.db.GetWebhook(id)
	if err != nil {
		return types.Webhook{}, err
	}

	current.Secret = newWebhookSecret()

	updated, err := s.db.UpdateWebhook(current)
	if err != nil {
		return types.Webhook{}, err
	}

	return updated, s.loadWebhooks()
}

// ListWebhookDeliveries returns the endpoint's kept delivery history,
// newest first.
func (s *State) ListWebhookDeliveries(id types.WebhookID) ([]types.WebhookDelivery, error) {
	_, err := s.db.GetWebhook(id)
	if err != nil {
		return nil, err
	}

	return s.db.ListWebhookDeliveries(id)
}

// DeleteWebhook removes an endpoint and stops delivering to it.
func (s *State) DeleteWebhook(id types.WebhookID) error {
	err := s.db.DeleteWebhook(id)
	if err != nil {
		return err
	}

	return s.loadWebhooks()
}

// TestWebhook posts a test event to the endpoint now and reports how it
// went; the delivery status is recorded like any other.
func (s *State) TestWebhook(ctx context.Context, id types.WebhookID) error {
	w, err := s.db.GetWebhook(id)
	if err != nil {
		return err
	}

	err = s.webhooks.Test(ctx, w)

	reloadErr := s.loadWebhooks()
	if reloadErr != nil {
		log.Error().Err(reloadErr).Msg("reloading webhooks after test")
	}

	return err
}

// emit hands an event to the dispatcher. It is safe on a nil or closed
// dispatcher so the sources never need to care.
func (s *State) emit(t types.WebhookEventType, message string, data any) {
	if s == nil || s.webhooks == nil {
		return
	}

	s.webhooks.Emit(t, message, data)
}

// webhookNodeData is the data an event about a node carries.
type webhookNodeData struct {
	NodeID    string   `json:"nodeId"`
	Name      string   `json:"name"`
	Hostname  string   `json:"hostname"`
	Addresses []string `json:"addresses"`
	User      string   `json:"user,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	ExpiresAt string   `json:"expiresAt,omitempty"`
}

func nodeEventData(node types.NodeView) webhookNodeData {
	data := webhookNodeData{
		NodeID:    node.ID().String(),
		Name:      node.GivenName(),
		Hostname:  node.Hostname(),
		Addresses: []string{},
		Tags:      slices.Clone(node.Tags().AsSlice()),
	}

	for _, ip := range node.IPs() {
		data.Addresses = append(data.Addresses, ip.String())
	}

	if node.User().Valid() {
		data.User = node.User().Name()
	}

	if node.Expiry().Valid() {
		data.ExpiresAt = node.Expiry().Get().UTC().Format(time.RFC3339)
	}

	return data
}

func nodeLabel(node types.NodeView) string {
	label := node.GivenName()
	if node.User().Valid() {
		label += " (" + node.User().Name() + ")"
	}

	return label
}

// webhookUserData is the data an event about a user carries.
type webhookUserData struct {
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role,omitempty"`
}

func userEventData(user *types.User) webhookUserData {
	return webhookUserData{
		UserID:      strconv.FormatUint(uint64(user.ID), 10),
		Name:        user.Name,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Role:        string(user.Role),
	}
}

func (s *State) emitNodeCreated(node types.NodeView) {
	s.emit(types.EventNodeCreated, fmt.Sprintf("Node %s joined the tailnet.", nodeLabel(node)), nodeEventData(node))

	if !node.IsApproved() {
		s.emit(types.EventNodeNeedsApproval,
			fmt.Sprintf("Node %s is waiting for approval.", nodeLabel(node)), nodeEventData(node))
	}
}

func (s *State) emitNodeApproved(node types.NodeView) {
	s.emit(types.EventNodeApproved, fmt.Sprintf("Node %s was approved.", nodeLabel(node)), nodeEventData(node))
}

func (s *State) emitNodeDeleted(node types.NodeView) {
	s.emit(types.EventNodeDeleted, fmt.Sprintf("Node %s was removed.", nodeLabel(node)), nodeEventData(node))
}

func (s *State) emitNodeKeyExpired(node types.NodeView) {
	s.emit(types.EventNodeKeyExpired, fmt.Sprintf("The key of node %s expired.", nodeLabel(node)), nodeEventData(node))
}

func (s *State) emitUserCreated(user *types.User) {
	s.emit(types.EventUserCreated, fmt.Sprintf("User %s was created.", user.Name), userEventData(user))

	if user.ApprovedAt == nil {
		s.emit(types.EventUserNeedsApproval,
			fmt.Sprintf("User %s is waiting for approval.", user.Name), userEventData(user))
	}
}

func (s *State) emitUserApproved(user *types.User) {
	s.emit(types.EventUserApproved, fmt.Sprintf("User %s was approved.", user.Name), userEventData(user))
}

func (s *State) emitUserRoleUpdated(user *types.User) {
	s.emit(types.EventUserRoleUpdated,
		fmt.Sprintf("User %s is now %s.", user.Name, user.Role), userEventData(user))
}

func (s *State) emitUserDeleted(user *types.User) {
	s.emit(types.EventUserDeleted, fmt.Sprintf("User %s was deleted.", user.Name), userEventData(user))
}

func (s *State) emitPolicyUpdate() {
	s.emit(types.EventPolicyUpdate, "The policy changed.", nil)
}
