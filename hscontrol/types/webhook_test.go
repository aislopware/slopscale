package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApproversRecipientSurvivesValidation(t *testing.T) {
	t.Parallel()

	endpoint := Webhook{
		ProviderType:  WebhookProviderEmail,
		URL:           "mailto:approvers",
		Subscriptions: []WebhookEventType{EventAccessRequestCreated},
	}

	require.NoError(t, ValidateWebhook(endpoint), "the token is not an address and must not be parsed as one")
	assert.Equal(t, []string{RecipientApprovers}, endpoint.Recipients(), "resolved at send time, not here")
	assert.True(t, endpoint.NotifiesApprovers())

	// The token may sit beside real addresses, which are still checked.
	mixed := endpoint
	mixed.URL = "mailto:approvers, ops@example.com"
	require.NoError(t, ValidateWebhook(mixed))
	assert.Equal(t, []string{RecipientApprovers, "ops@example.com"}, mixed.Recipients())

	bad := endpoint
	bad.URL = "mailto:approvers, not an address"
	require.ErrorIs(t, ValidateWebhook(bad), ErrWebhookMailtoInvalid)

	plain := Webhook{ProviderType: WebhookProviderSlack, URL: "https://hooks.example.com/x"}
	assert.False(t, plain.NotifiesApprovers(), "a posting endpoint has no recipients at all")
}
