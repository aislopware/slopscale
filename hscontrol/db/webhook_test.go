package db

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
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
	created.Secret = "hswh-ignored"

	updated, err := hsdb.UpdateWebhook(created)
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example/def", updated.URL)
	assert.Equal(t, "hswh-secret", updated.Secret, "an edit leaves the secret alone")
	assert.Equal(t, []types.WebhookEventType{types.EventPolicyUpdate}, updated.Subscriptions)

	rotated, err := hsdb.SetWebhookSecret(created.ID, "hswh-rotated")
	require.NoError(t, err)
	assert.Equal(t, "hswh-rotated", rotated.Secret)
	assert.Equal(t, "https://hooks.example/def", rotated.URL, "a rotation leaves the other fields alone")

	_, err = hsdb.SetWebhookSecret(99999, "hswh-nobody")
	require.ErrorIs(t, err, types.ErrWebhookNotFound)

	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	require.NoError(t, hsdb.RecordWebhookDelivery(types.WebhookDelivery{
		WebhookID: created.ID,
		EventType: types.EventPolicyUpdate,
		Status:    "200",
		OK:        true,
		Attempts:  2,
		Duration:  1500 * time.Millisecond,
		At:        at,
	}))

	list, err := hsdb.ListWebhooks()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "200", list[0].LastDeliveryStatus)
	require.NotNil(t, list[0].LastDeliveryAt)
	assert.True(t, list[0].LastDeliveryAt.Equal(at))

	deliveries, err := hsdb.ListWebhookDeliveries(created.ID)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	assert.Equal(t, types.EventPolicyUpdate, deliveries[0].EventType)
	assert.Equal(t, 2, deliveries[0].Attempts)
	assert.Equal(t, 1500*time.Millisecond, deliveries[0].Duration)
	assert.True(t, deliveries[0].OK)
	assert.True(t, deliveries[0].At.Equal(at))

	require.NoError(t, hsdb.DeleteWebhook(created.ID))

	deliveries, err = hsdb.ListWebhookDeliveries(created.ID)
	require.NoError(t, err)
	assert.Empty(t, deliveries, "deleting the webhook cascades to its history")
	require.ErrorIs(t, hsdb.DeleteWebhook(created.ID), types.ErrWebhookNotFound)

	_, err = hsdb.GetWebhook(created.ID)
	require.ErrorIs(t, err, types.ErrWebhookNotFound)
}

// TestWebhookDeliveryHistoryTrims keeps only the newest
// [types.WebhookDeliveryHistory] rows per endpoint.
func TestWebhookDeliveryHistoryTrims(t *testing.T) {
	t.Parallel()

	hsdb, err := newSQLiteTestDB()
	require.NoError(t, err)

	hook, err := hsdb.CreateWebhook(types.Webhook{
		URL:           "https://example.com/trim",
		Secret:        "s",
		Subscriptions: []types.WebhookEventType{types.EventNodeCreated},
	})
	require.NoError(t, err)

	other, err := hsdb.CreateWebhook(types.Webhook{
		URL:           "https://example.com/other",
		Secret:        "s",
		Subscriptions: []types.WebhookEventType{types.EventNodeCreated},
	})
	require.NoError(t, err)

	base := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	for i := range types.WebhookDeliveryHistory + 5 {
		require.NoError(t, hsdb.RecordWebhookDelivery(types.WebhookDelivery{
			WebhookID: hook.ID,
			EventType: types.EventNodeCreated,
			Status:    "204",
			OK:        true,
			Attempts:  1,
			At:        base.Add(time.Duration(i) * time.Second),
		}))
	}

	require.NoError(t, hsdb.RecordWebhookDelivery(types.WebhookDelivery{
		WebhookID: other.ID, EventType: types.EventNodeCreated, Status: "500", Attempts: 4, At: base,
	}))

	deliveries, err := hsdb.ListWebhookDeliveries(hook.ID)
	require.NoError(t, err)
	require.Len(t, deliveries, types.WebhookDeliveryHistory)
	assert.True(t, deliveries[0].At.After(deliveries[len(deliveries)-1].At), "newest first")
	assert.True(t, deliveries[len(deliveries)-1].At.Equal(base.Add(5*time.Second)), "the oldest five were dropped")

	otherDeliveries, err := hsdb.ListWebhookDeliveries(other.ID)
	require.NoError(t, err)
	require.Len(t, otherDeliveries, 1, "trimming is per endpoint")
	assert.False(t, otherDeliveries[0].OK)
}
