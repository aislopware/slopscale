package db

import (
	"encoding/json"
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

// webhookRow is a row of the webhooks table; see schema.sql.
type webhookRow struct {
	ID                 uint64 `sql:"primary_key"`
	URL                string
	Description        string
	ProviderType       string
	Secret             string
	Subscriptions      string
	CreatedBy          *uint64
	CreatedAt          *time.Time
	UpdatedAt          *time.Time
	LastDeliveryAt     *time.Time
	LastDeliveryStatus string
}

type webhookRecord struct {
	Webhook webhookRow `alias:"webhooks"`
}

func (r webhookRow) webhook() (types.Webhook, error) {
	w := types.Webhook{
		ID:                 types.WebhookID(r.ID),
		URL:                r.URL,
		Description:        r.Description,
		ProviderType:       types.WebhookProvider(r.ProviderType),
		Secret:             r.Secret,
		LastDeliveryAt:     r.LastDeliveryAt,
		LastDeliveryStatus: r.LastDeliveryStatus,
	}

	if r.CreatedBy != nil {
		w.CreatedBy = types.UserID(*r.CreatedBy)
	}

	if r.CreatedAt != nil {
		w.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		w.UpdatedAt = *r.UpdatedAt
	}

	err := json.Unmarshal([]byte(r.Subscriptions), &w.Subscriptions)
	if err != nil {
		return types.Webhook{}, fmt.Errorf("webhook %d holds subscriptions %q: %w", r.ID, r.Subscriptions, err)
	}

	return w, nil
}

func webhookRowFrom(w types.Webhook) (webhookRow, error) {
	subs, err := json.Marshal(w.Subscriptions)
	if err != nil {
		return webhookRow{}, fmt.Errorf("encoding subscriptions: %w", err)
	}

	row := webhookRow{
		URL:           w.URL,
		Description:   w.Description,
		ProviderType:  string(w.ProviderType),
		Secret:        w.Secret,
		Subscriptions: string(subs),
	}

	if w.CreatedBy != 0 {
		id := uint64(w.CreatedBy)
		row.CreatedBy = &id
	}

	return row, nil
}

// ListWebhooks reads every webhook in ID order.
func (hsdb *HSDatabase) ListWebhooks() ([]types.Webhook, error) {
	var records []webhookRecord

	err := hsdb.ex.query(
		jet.SELECT(table.Webhooks.AllColumns).FROM(table.Webhooks).ORDER_BY(table.Webhooks.ID.ASC()), &records,
	)
	if err != nil {
		return nil, fmt.Errorf("listing webhooks: %w", err)
	}

	out := make([]types.Webhook, 0, len(records))

	for _, r := range records {
		w, err := r.Webhook.webhook()
		if err != nil {
			return nil, err
		}

		out = append(out, w)
	}

	return out, nil
}

// GetWebhook reads one webhook.
func (hsdb *HSDatabase) GetWebhook(id types.WebhookID) (types.Webhook, error) {
	return getWebhook(hsdb, id)
}

func getWebhook(q Querier, id types.WebhookID) (types.Webhook, error) {
	var records []webhookRecord

	err := q.executor().query(
		jet.SELECT(table.Webhooks.AllColumns).FROM(table.Webhooks).
			WHERE(table.Webhooks.ID.EQ(jet.Uint64(uint64(id)))),
		&records,
	)
	if err != nil {
		return types.Webhook{}, fmt.Errorf("reading webhook %d: %w", id, err)
	}

	if len(records) == 0 {
		return types.Webhook{}, types.ErrWebhookNotFound
	}

	return records[0].Webhook.webhook()
}

// CreateWebhook stores a webhook and returns it with its ID.
func (hsdb *HSDatabase) CreateWebhook(w types.Webhook) (types.Webhook, error) {
	return Write(hsdb, func(tx *Tx) (types.Webhook, error) {
		row, err := webhookRowFrom(w)
		if err != nil {
			return types.Webhook{}, err
		}

		now := time.Now().UTC()
		row.CreatedAt = &now
		row.UpdatedAt = &now

		var inserted idRow

		err = tx.executor().query(
			table.Webhooks.INSERT(table.Webhooks.MutableColumns).MODEL(&row).
				RETURNING(table.Webhooks.ID.AS("id_row.id")),
			&inserted,
		)
		if err != nil {
			return types.Webhook{}, fmt.Errorf("creating webhook: %w", err)
		}

		return getWebhook(tx, types.WebhookID(inserted.ID))
	})
}

// UpdateWebhook replaces the URL, description, provider, secret and
// subscriptions of the webhook.
func (hsdb *HSDatabase) UpdateWebhook(w types.Webhook) (types.Webhook, error) {
	return Write(hsdb, func(tx *Tx) (types.Webhook, error) {
		row, err := webhookRowFrom(w)
		if err != nil {
			return types.Webhook{}, err
		}

		affected, err := tx.executor().exec(
			table.Webhooks.UPDATE(
				table.Webhooks.URL, table.Webhooks.Description, table.Webhooks.ProviderType,
				table.Webhooks.Secret, table.Webhooks.Subscriptions, table.Webhooks.UpdatedAt,
			).SET(
				row.URL, row.Description, row.ProviderType, row.Secret, row.Subscriptions, time.Now().UTC(),
			).WHERE(table.Webhooks.ID.EQ(jet.Uint64(uint64(w.ID)))),
		)
		if err != nil {
			return types.Webhook{}, fmt.Errorf("updating webhook %d: %w", w.ID, err)
		}

		if affected == 0 {
			return types.Webhook{}, types.ErrWebhookNotFound
		}

		return getWebhook(tx, w.ID)
	})
}

// RecordWebhookDelivery stores how the newest delivery went. It is
// called from the delivery goroutines, so it never fails loudly.
func (hsdb *HSDatabase) RecordWebhookDelivery(id types.WebhookID, at time.Time, status string) error {
	return hsdb.Write(func(tx *Tx) error {
		_, err := tx.executor().exec(
			table.Webhooks.UPDATE(table.Webhooks.LastDeliveryAt, table.Webhooks.LastDeliveryStatus).
				SET(at.UTC(), status).
				WHERE(table.Webhooks.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("recording webhook %d delivery: %w", id, err)
		}

		return nil
	})
}

// DeleteWebhook removes the webhook.
func (hsdb *HSDatabase) DeleteWebhook(id types.WebhookID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.Webhooks.DELETE().WHERE(table.Webhooks.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting webhook %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrWebhookNotFound
		}

		return nil
	})
}
