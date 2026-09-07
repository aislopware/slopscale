package types

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// WebhookID identifies a webhook endpoint in the webhooks table.
type WebhookID uint64

// String renders the ID in base 10.
func (id WebhookID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Webhook is an HTTP endpoint the server posts events to, the way
// Tailscale's webhooks work: a URL, the event types it subscribes to and
// a secret the payloads are signed with.
type Webhook struct {
	ID          WebhookID
	URL         string
	Description string
	// ProviderType shapes the payload for a chat service; empty is the
	// generic signed JSON.
	ProviderType WebhookProvider
	// Secret signs every delivery; the operator sees it once.
	Secret        string
	Subscriptions []WebhookEventType
	// CreatedBy is the user that created the endpoint; zero for a
	// credential without one.
	CreatedBy UserID
	CreatedAt time.Time
	UpdatedAt time.Time
	// LastDeliveryAt and LastDeliveryStatus report the newest attempt;
	// the status is the HTTP status, or the error text when none came.
	LastDeliveryAt     *time.Time
	LastDeliveryStatus string
}

// WebhookDeliveryID is the identifier of one delivery record.
type WebhookDeliveryID uint64

// WebhookDelivery is how one event's delivery to one endpoint went, after
// every retry.
type WebhookDelivery struct {
	ID        WebhookDeliveryID
	WebhookID WebhookID
	EventType WebhookEventType
	// Status is the HTTP status, or the error text when no response came.
	Status string
	// OK is whether the receiver answered 2xx in the end.
	OK bool
	// Attempts counts the requests made, retries included.
	Attempts int
	Duration time.Duration
	At       time.Time
}

// WebhookDeliveryHistory is how many deliveries are kept per endpoint.
const WebhookDeliveryHistory = 100

// Host is the URL's host, which is safe to log and audit; the path and
// query of a chat provider's URL are its credential.
func (w Webhook) Host() string {
	u, err := url.Parse(w.URL)
	if err != nil {
		return ""
	}

	return u.Host
}

// Subscribed reports whether the endpoint wants the event type.
func (w Webhook) Subscribed(t WebhookEventType) bool {
	return slices.Contains(w.Subscriptions, t)
}

// WebhookProvider names the chat service a webhook posts to.
type WebhookProvider string

// The providers Tailscale formats payloads for.
const (
	WebhookProviderGeneric    WebhookProvider = ""
	WebhookProviderSlack      WebhookProvider = "slack"
	WebhookProviderMattermost WebhookProvider = "mattermost"
	WebhookProviderGoogleChat WebhookProvider = "googlechat"
	WebhookProviderDiscord    WebhookProvider = "discord"
)

// WebhookProviders lists every provider the server accepts.
var WebhookProviders = []WebhookProvider{
	WebhookProviderGeneric,
	WebhookProviderSlack,
	WebhookProviderMattermost,
	WebhookProviderGoogleChat,
	WebhookProviderDiscord,
}

// WebhookEventType names something a webhook can subscribe to. The names
// are Tailscale's so the same receivers work.
type WebhookEventType string

// The event types the server raises.
const (
	EventNodeCreated       WebhookEventType = "nodeCreated"
	EventNodeNeedsApproval WebhookEventType = "nodeNeedsApproval"
	EventNodeApproved      WebhookEventType = "nodeApproved"
	EventNodeKeyExpired    WebhookEventType = "nodeKeyExpired"
	EventNodeDeleted       WebhookEventType = "nodeDeleted"
	// EventNodeSuspended and EventNodeUnsuspended have no Tailscale
	// counterpart; see docs/ref/device-trust.md.
	EventNodeSuspended     WebhookEventType = "nodeSuspended"
	EventNodeUnsuspended   WebhookEventType = "nodeUnsuspended"
	EventPolicyUpdate      WebhookEventType = "policyUpdate"
	EventUserCreated       WebhookEventType = "userCreated"
	EventUserNeedsApproval WebhookEventType = "userNeedsApproval"
	EventUserApproved      WebhookEventType = "userApproved"
	EventUserRoleUpdated   WebhookEventType = "userRoleUpdated"
	EventUserDeleted       WebhookEventType = "userDeleted"
	// EventTest is what the test endpoint sends; every webhook gets it.
	EventTest WebhookEventType = "test"
)

// WebhookEventTypes lists every subscribable event type, in display order.
var WebhookEventTypes = []WebhookEventType{
	EventNodeCreated,
	EventNodeNeedsApproval,
	EventNodeApproved,
	EventNodeKeyExpired,
	EventNodeDeleted,
	EventNodeSuspended,
	EventNodeUnsuspended,
	EventPolicyUpdate,
	EventUserCreated,
	EventUserNeedsApproval,
	EventUserApproved,
	EventUserRoleUpdated,
	EventUserDeleted,
}

// WebhookEvent is one delivery's payload, in Tailscale's shape.
type WebhookEvent struct {
	Timestamp time.Time        `json:"timestamp"`
	Version   int              `json:"version"`
	Type      WebhookEventType `json:"type"`
	Tailnet   string           `json:"tailnet"`
	Message   string           `json:"message"`
	Data      any              `json:"data,omitempty"`
}

// WebhookEventVersion is the payload version the server sends.
const WebhookEventVersion = 1

// Errors returned by the webhook validation.
var (
	ErrWebhookURLInvalid      = errors.New("webhook URL must be an http or https URL")
	ErrWebhookNoSubscriptions = errors.New("webhook needs at least one subscription")
	ErrWebhookEventUnknown    = errors.New("unknown webhook event type")
	ErrWebhookProviderUnknown = errors.New("unknown webhook provider")
	ErrWebhookNotFound        = errors.New("webhook not found")
	ErrWebhookDescriptionLong = errors.New("webhook description must be at most 200 characters")
)

const maxWebhookDescriptionRunes = 200

// ParseWebhookSubscriptions checks and dedupes event type names.
func ParseWebhookSubscriptions(names []string) ([]WebhookEventType, error) {
	out := make([]WebhookEventType, 0, len(names))

	for _, name := range names {
		t := WebhookEventType(strings.TrimSpace(name))
		if !slices.Contains(WebhookEventTypes, t) {
			return nil, fmt.Errorf("%w: %q", ErrWebhookEventUnknown, name)
		}

		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}

	return out, nil
}

// ValidateWebhook checks the fields the operator controls.
func ValidateWebhook(w Webhook) error {
	u, err := url.Parse(w.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q", ErrWebhookURLInvalid, w.URL)
	}

	if len(w.Subscriptions) == 0 {
		return ErrWebhookNoSubscriptions
	}

	if !slices.Contains(WebhookProviders, w.ProviderType) {
		return fmt.Errorf("%w: %q", ErrWebhookProviderUnknown, w.ProviderType)
	}

	if len([]rune(w.Description)) > maxWebhookDescriptionRunes {
		return ErrWebhookDescriptionLong
	}

	return nil
}
