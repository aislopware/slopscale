package db

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookRoundTrip(t *testing.T) {
	t.Parallel()

	hsdb, err := newSQLiteTestDB()
	require.NoError(t, err)

	created, err := hsdb.CreateWebhook(types.Webhook{
		URL:           "https://hooks.example/abc",
		Description:   "ops channel",
		ProviderType:  types.WebhookProviderSlack,
		Secret:        "hswh-secret",
		Subscriptions: []types.WebhookEventType{types.EventNodeCreated, types.EventUserDeleted},
		CreatedBy:     1,
	})
	require.NoError(t, err)
	assert.NotZero(t, created.ID)
	assert.Equal(t, types.UserID(1), created.CreatedBy)
	assert.Equal(t, []types.WebhookEventType{types.EventNodeCreated, types.EventUserDeleted}, created.Subscriptions)
	assert.Nil(t, created.LastDeliveryAt)

	created.URL = "https://hooks.example/def"
	created.Subscriptions = []types.WebhookEventType{types.EventPolicyUpdate}
	created.Secret = "hswh-rotated"

	updated, err := hsdb.UpdateWebhook(created)
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example/def", updated.URL)
	assert.Equal(t, "hswh-rotated", updated.Secret)
	assert.Equal(t, []types.WebhookEventType{types.EventPolicyUpdate}, updated.Subscriptions)

	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	require.NoError(t, hsdb.RecordWebhookDelivery(created.ID, at, "200"))

	list, err := hsdb.ListWebhooks()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "200", list[0].LastDeliveryStatus)
	require.NotNil(t, list[0].LastDeliveryAt)
	assert.True(t, list[0].LastDeliveryAt.Equal(at))

	require.NoError(t, hsdb.DeleteWebhook(created.ID))
	require.ErrorIs(t, hsdb.DeleteWebhook(created.ID), types.ErrWebhookNotFound)

	_, err = hsdb.GetWebhook(created.ID)
	require.ErrorIs(t, err, types.ErrWebhookNotFound)
}
