package types

import (
	"errors"
	"fmt"
	"net/mail"
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
// query of a chat provider's URL are its credential. For an email
// endpoint it is the recipients.
func (w Webhook) Host() string {
	u, err := url.Parse(w.URL)
	if err != nil {
		return ""
	}

	if u.Scheme == mailtoScheme {
		return u.Opaque
	}

	return u.Host
}

// Recipients returns the addresses of an email endpoint, the comma
// separated list after "mailto:".
func (w Webhook) Recipients() []string {
	u, err := url.Parse(w.URL)
	if err != nil || u.Scheme != mailtoScheme {
		return nil
	}

	return splitAddresses(u.Opaque)
}

func splitAddresses(list string) []string {
	var out []string

	for addr := range strings.SplitSeq(list, ",") {
		if addr = strings.TrimSpace(addr); addr != "" {
			out = append(out, addr)
		}
	}

	return out
}

// Subscribed reports whether the endpoint wants the event type.
func (w Webhook) Subscribed(t WebhookEventType) bool {
	return slices.Contains(w.Subscriptions, t)
}

// WebhookProvider names the service a webhook posts to, which decides
// the payload's shape and how it travels.
type WebhookProvider string

// The providers Tailscale formats payloads for, and the notification
// channels that have no Tailscale counterpart (see docs/ref/webhooks.md).
const (
	WebhookProviderGeneric    WebhookProvider = ""
	WebhookProviderSlack      WebhookProvider = "slack"
	WebhookProviderMattermost WebhookProvider = "mattermost"
	WebhookProviderGoogleChat WebhookProvider = "googlechat"
	WebhookProviderDiscord    WebhookProvider = "discord"
	// WebhookProviderTeams posts {"text"} to a Microsoft Teams incoming
	// webhook or workflow.
	WebhookProviderTeams WebhookProvider = "teams"
	// WebhookProviderTelegram posts to the Bot API's sendMessage URL; the
	// chat_id query parameter of the URL names the chat.
	WebhookProviderTelegram WebhookProvider = "telegram"
	// WebhookProviderNtfy posts the message to an ntfy topic URL.
	WebhookProviderNtfy WebhookProvider = "ntfy"
	// WebhookProviderEmail sends the event by mail to the "mailto:"
	// recipients through the configured SMTP server.
	WebhookProviderEmail WebhookProvider = "email"
)

// WebhookProviders lists every provider the server accepts.
var WebhookProviders = []WebhookProvider{
	WebhookProviderGeneric,
	WebhookProviderSlack,
	WebhookProviderMattermost,
	WebhookProviderGoogleChat,
	WebhookProviderDiscord,
	WebhookProviderTeams,
	WebhookProviderTelegram,
	WebhookProviderNtfy,
	WebhookProviderEmail,
}

// TelegramChatParam is the query parameter of a Telegram endpoint's URL
// that names the chat; the dispatcher moves it into the request body.
const TelegramChatParam = "chat_id"

const mailtoScheme = "mailto"

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
	// EventAccessRequestCreated, EventAccessRequestApproved and
	// EventAccessRequestDenied have no Tailscale counterpart; see
	// docs/ref/temporary-access.md.
	EventAccessRequestCreated  WebhookEventType = "accessRequestCreated"
	EventAccessRequestApproved WebhookEventType = "accessRequestApproved"
	EventAccessRequestDenied   WebhookEventType = "accessRequestDenied"
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
	EventAccessRequestCreated,
	EventAccessRequestApproved,
	EventAccessRequestDenied,
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
	ErrWebhookMailtoInvalid   = errors.New("email webhook URL must be mailto: followed by the recipients")
	ErrWebhookTelegramNoChat  = errors.New("telegram webhook URL needs a chat_id query parameter")
	ErrWebhookMailUnavailable = errors.New("email webhooks need notifications.smtp configured")
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
	if !slices.Contains(WebhookProviders, w.ProviderType) {
		return fmt.Errorf("%w: %q", ErrWebhookProviderUnknown, w.ProviderType)
	}

	err := validateWebhookURL(w)
	if err != nil {
		return err
	}

	if len(w.Subscriptions) == 0 {
		return ErrWebhookNoSubscriptions
	}

	if len([]rune(w.Description)) > maxWebhookDescriptionRunes {
		return ErrWebhookDescriptionLong
	}

	return nil
}

// validateWebhookURL checks the URL against what the provider needs: an
// http(s) URL for the posting kinds, a Telegram URL with its chat, or a
// mailto: list for email.
func validateWebhookURL(w Webhook) error {
	u, err := url.Parse(w.URL)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrWebhookURLInvalid, w.URL)
	}

	if w.ProviderType == WebhookProviderEmail {
		if u.Scheme != mailtoScheme || len(splitAddresses(u.Opaque)) == 0 {
			return fmt.Errorf("%w: %q", ErrWebhookMailtoInvalid, w.URL)
		}

		for _, addr := range splitAddresses(u.Opaque) {
			_, err := mail.ParseAddress(addr)
			if err != nil {
				return fmt.Errorf("%w: %q", ErrWebhookMailtoInvalid, addr)
			}
		}

		return nil
	}

	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q", ErrWebhookURLInvalid, w.URL)
	}

	if w.ProviderType == WebhookProviderTelegram && u.Query().Get(TelegramChatParam) == "" {
		return ErrWebhookTelegramNoChat
	}

	return nil
}
