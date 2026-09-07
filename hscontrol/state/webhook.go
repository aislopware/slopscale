package state

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
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

// loadWebhooks reads the endpoints into the dispatcher. Callers that
// change endpoints hold webhookMu across the write and this reload, so
// the dispatcher never publishes a list older than the last write.
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

	s.webhookMu.Lock()
	defer s.webhookMu.Unlock()

	created, err := s.db.CreateWebhook(w)
	if err != nil {
		return types.Webhook{}, err
	}

	return created, s.loadWebhooks()
}

// UpdateWebhook replaces the URL, description, provider and
// subscriptions of an endpoint; the secret stays. The secret column is
// not written, so an edit cannot undo a rotation.
func (s *State) UpdateWebhook(w types.Webhook) (types.Webhook, error) {
	w.URL = strings.TrimSpace(w.URL)
	// Validation wants a secret; the stored one is not read here.
	w.Secret = newWebhookSecret()

	err := types.ValidateWebhook(w)
	if err != nil {
		return types.Webhook{}, err
	}

	s.webhookMu.Lock()
	defer s.webhookMu.Unlock()

	updated, err := s.db.UpdateWebhook(w)
	if err != nil {
		return types.Webhook{}, err
	}

	return updated, s.loadWebhooks()
}

// RotateWebhookSecret replaces the endpoint's secret and returns the
// record with the new one.
func (s *State) RotateWebhookSecret(id types.WebhookID) (types.Webhook, error) {
	s.webhookMu.Lock()
	defer s.webhookMu.Unlock()

	updated, err := s.db.SetWebhookSecret(id, newWebhookSecret())
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
	s.webhookMu.Lock()
	defer s.webhookMu.Unlock()

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

	s.webhookMu.Lock()
	defer s.webhookMu.Unlock()

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

// webhookNodeData is the data an event about a node carries, named as
// Tailscale names it so a receiver written for Tailscale reads it as is.
// addresses and tags are extra.
type webhookNodeData struct {
	NodeID     string   `json:"nodeID"`
	DeviceName string   `json:"deviceName"`
	ManagedBy  string   `json:"managedBy"`
	URL        string   `json:"url"`
	Expiration string   `json:"expiration,omitempty"`
	Addresses  []string `json:"addresses"`
	Tags       []string `json:"tags,omitempty"`
}

// managedByTags is what Tailscale reports as the manager of a tagged node.
const managedByTags = "tagged-devices"

func (s *State) nodeEventData(node types.NodeView) webhookNodeData {
	data := webhookNodeData{
		NodeID:     node.ID().String(),
		DeviceName: node.GivenName(),
		ManagedBy:  managedByTags,
		URL:        s.consoleURL("machines/" + node.ID().String()),
		Addresses:  []string{},
		Tags:       slices.Clone(node.Tags().AsSlice()),
	}

	fqdn, err := node.GetFQDN(s.cfg.BaseDomain)
	if err == nil && fqdn != "" {
		data.DeviceName = fqdn
	}

	if !node.IsTagged() && node.User().Valid() {
		data.ManagedBy = userLabel(node.User().AsStruct())
	}

	for _, ip := range node.IPs() {
		data.Addresses = append(data.Addresses, ip.String())
	}

	if node.Expiry().Valid() && !node.Expiry().Get().IsZero() {
		data.Expiration = node.Expiry().Get().UTC().Format(time.RFC3339)
	}

	return data
}

// consoleURL is the admin console page for a path, the way Tailscale's
// events link to its console.
func (s *State) consoleURL(path string) string {
	if s.cfg == nil || s.cfg.ServerURL == "" {
		return ""
	}

	return strings.TrimSuffix(s.cfg.ServerURL, "/") + "/admin/" + path
}

// userLabel names a user the way Tailscale's events do: the email when
// there is one, the login name otherwise.
func userLabel(user *types.User) string {
	return cmp.Or(user.Email, user.Name)
}

func nodeLabel(node types.NodeView) string {
	label := node.GivenName()
	if node.User().Valid() {
		label += " (" + node.User().Name() + ")"
	}

	return label
}

// webhookUserData is the data an event about a user carries. user is the
// name Tailscale uses; the rest is extra.
type webhookUserData struct {
	User        string   `json:"user"`
	URL         string   `json:"url"`
	Actor       string   `json:"actor,omitempty"`
	OldRoles    []string `json:"oldRoles,omitempty"`
	NewRoles    []string `json:"newRoles,omitempty"`
	UserID      string   `json:"userID"`
	DisplayName string   `json:"displayName,omitempty"`
}

func (s *State) userEventData(user *types.User) webhookUserData {
	return webhookUserData{
		User:        userLabel(user),
		URL:         s.consoleURL("users?q=" + url.QueryEscape(user.Name)),
		UserID:      strconv.FormatUint(uint64(user.ID), 10),
		DisplayName: user.DisplayName,
	}
}

func (s *State) emitNodeCreated(node types.NodeView) {
	s.emit(types.EventNodeCreated, fmt.Sprintf("Node %s joined the tailnet.", nodeLabel(node)), s.nodeEventData(node))

	if !node.IsApproved() {
		s.emit(types.EventNodeNeedsApproval,
			fmt.Sprintf("Node %s is waiting for approval.", nodeLabel(node)), s.nodeEventData(node))
	}
}

func (s *State) emitNodeApproved(node types.NodeView) {
	s.emit(types.EventNodeApproved, fmt.Sprintf("Node %s was approved.", nodeLabel(node)), s.nodeEventData(node))
}

func (s *State) emitNodeDeleted(node types.NodeView) {
	s.emit(types.EventNodeDeleted, fmt.Sprintf("Node %s was removed.", nodeLabel(node)), s.nodeEventData(node))
}

func (s *State) emitNodeKeyExpired(node types.NodeView) {
	s.emit(
		types.EventNodeKeyExpired,
		fmt.Sprintf("The key of node %s expired.", nodeLabel(node)),
		s.nodeEventData(node),
	)
}

func (s *State) emitUserCreated(user *types.User) {
	s.emit(types.EventUserCreated, fmt.Sprintf("User %s was created.", user.Name), s.userEventData(user))

	if user.ApprovedAt == nil {
		s.emit(types.EventUserNeedsApproval,
			fmt.Sprintf("User %s is waiting for approval.", user.Name), s.userEventData(user))
	}
}

func (s *State) emitUserApproved(user *types.User) {
	s.emit(types.EventUserApproved, fmt.Sprintf("User %s was approved.", user.Name), s.userEventData(user))
}

// emitUserRoleUpdated reports a role change with the roles before and
// after and, when known, who made it.
func (s *State) emitUserRoleUpdated(user *types.User, previous types.Role, actor *RoleActor) {
	data := s.userEventData(user)
	data.OldRoles = []string{string(previous)}
	data.NewRoles = []string{string(user.Role)}

	if actor != nil {
		who, err := s.GetUserByID(actor.UserID)
		if err == nil {
			data.Actor = userLabel(who)
		}
	}

	s.emit(types.EventUserRoleUpdated, fmt.Sprintf("User %s is now %s.", user.Name, user.Role), data)
}

func (s *State) emitUserDeleted(user *types.User) {
	s.emit(types.EventUserDeleted, fmt.Sprintf("User %s was deleted.", user.Name), s.userEventData(user))
}

func (s *State) emitPolicyUpdate() {
	s.emit(types.EventPolicyUpdate, "The policy changed.", nil)
}
