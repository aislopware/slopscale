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

// webhookDeliveryRow is a row of the webhook_deliveries table; see
// schema.sql.
type webhookDeliveryRow struct {
	ID         uint64 `sql:"primary_key"`
	WebhookID  uint64
	EventType  string
	Status     string
	Ok         bool
	Attempts   int64
	DurationMs int64
	CreatedAt  time.Time
}

type webhookDeliveryRecord struct {
	Delivery webhookDeliveryRow `alias:"webhook_deliveries"`
}

func (r webhookDeliveryRow) delivery() types.WebhookDelivery {
	return types.WebhookDelivery{
		ID:        types.WebhookDeliveryID(r.ID),
		WebhookID: types.WebhookID(r.WebhookID),
		EventType: types.WebhookEventType(r.EventType),
		Status:    r.Status,
		OK:        r.Ok,
		Attempts:  int(r.Attempts),
		Duration:  time.Duration(r.DurationMs) * time.Millisecond,
		At:        r.CreatedAt,
	}
}

// RecordWebhookDelivery stores how a delivery went: on the endpoint as the
// newest status, and in the history, which is trimmed to
// [types.WebhookDeliveryHistory] rows. It is called from the delivery
// goroutines, so it never fails loudly.
func (hsdb *HSDatabase) RecordWebhookDelivery(d types.WebhookDelivery) error {
	return hsdb.Write(func(tx *Tx) error {
		at := d.At.UTC()

		_, err := tx.executor().exec(
			table.Webhooks.UPDATE(table.Webhooks.LastDeliveryAt, table.Webhooks.LastDeliveryStatus).
				SET(at, d.Status).
				WHERE(table.Webhooks.ID.EQ(jet.Uint64(uint64(d.WebhookID)))),
		)
		if err != nil {
			return fmt.Errorf("recording webhook %d delivery: %w", d.WebhookID, err)
		}

		row := webhookDeliveryRow{
			WebhookID:  uint64(d.WebhookID),
			EventType:  string(d.EventType),
			Status:     d.Status,
			Ok:         d.OK,
			Attempts:   int64(d.Attempts),
			DurationMs: d.Duration.Milliseconds(),
			CreatedAt:  at,
		}

		_, err = tx.executor().exec(
			table.WebhookDeliveries.INSERT(table.WebhookDeliveries.MutableColumns).MODEL(&row),
		)
		if err != nil {
			return fmt.Errorf("recording webhook %d delivery history: %w", d.WebhookID, err)
		}

		return trimWebhookDeliveries(tx, d.WebhookID)
	})
}

// trimWebhookDeliveries drops everything older than the newest
// [types.WebhookDeliveryHistory] rows of the endpoint.
func trimWebhookDeliveries(tx *Tx, id types.WebhookID) error {
	keep := table.WebhookDeliveries.
		SELECT(table.WebhookDeliveries.ID).
		WHERE(table.WebhookDeliveries.WebhookID.EQ(jet.Uint64(uint64(id)))).
		ORDER_BY(table.WebhookDeliveries.ID.DESC()).
		LIMIT(types.WebhookDeliveryHistory)

	_, err := tx.executor().exec(
		table.WebhookDeliveries.DELETE().
			WHERE(
				table.WebhookDeliveries.WebhookID.EQ(jet.Uint64(uint64(id))).
					AND(table.WebhookDeliveries.ID.NOT_IN(keep)),
			),
	)
	if err != nil {
		return fmt.Errorf("trimming webhook %d delivery history: %w", id, err)
	}

	return nil
}

// ListWebhookDeliveries returns the endpoint's kept deliveries, newest
// first.
func (hsdb *HSDatabase) ListWebhookDeliveries(id types.WebhookID) ([]types.WebhookDelivery, error) {
	var rows []webhookDeliveryRecord

	err := hsdb.executor().query(
		table.WebhookDeliveries.SELECT(table.WebhookDeliveries.AllColumns).
			WHERE(table.WebhookDeliveries.WebhookID.EQ(jet.Uint64(uint64(id)))).
			ORDER_BY(table.WebhookDeliveries.ID.DESC()),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("listing webhook %d deliveries: %w", id, err)
	}

	out := make([]types.WebhookDelivery, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Delivery.delivery())
	}

	return out, nil
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
