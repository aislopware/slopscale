package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/libdns/libdns"
)

const (
	// CloudflareAPI is the API base; tests point it elsewhere.
	CloudflareAPI = "https://api.cloudflare.com/client/v4"

	cloudflareTimeout = 30 * time.Second
	// maxErrorBody bounds what an error response contributes to the
	// error text.
	maxErrorBody = 4 << 10
	// maxResponseBody bounds a response; a page of 100 records fits.
	maxResponseBody = 1 << 20
	// cloudflareMinTTL is the shortest TTL the API takes besides 1, which
	// means automatic.
	cloudflareMinTTL = 60
)

var (
	// ErrZoneNotFound is returned when no zone holds the record's name.
	ErrZoneNotFound = errors.New("no Cloudflare zone holds the record name")
	// ErrCloudflareAPI wraps an answer the API refused.
	ErrCloudflareAPI = errors.New("cloudflare API error")

	errCloudflareToken    = errors.New("api_token is required")
	errCloudflareNotFound = errors.New("cloudflare record not found")
)

// Cloudflare publishes through the Cloudflare API with a scoped token.
type Cloudflare struct {
	// APIToken needs DNS:Edit on the zone, and Zone:Read unless ZoneID
	// is set.
	APIToken string `json:"api_token"`
	// ZoneID names the zone; empty finds it from the record's name.
	ZoneID string `json:"zone_id,omitempty"`

	base   string
	client *http.Client
}

// NewCloudflare builds the provider.
func NewCloudflare(apiToken, zoneID string) *Cloudflare {
	return &Cloudflare{APIToken: apiToken, ZoneID: zoneID}
}

// WithBase points the provider at another API base, for tests.
func (c *Cloudflare) WithBase(base string) *Cloudflare {
	c.base = base

	return c
}

type cfEnvelope[T any] struct {
	Success bool      `json:"success"`
	Errors  []cfError `json:"errors"`
	Result  T         `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cfRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
}

// AppendRecords adds each value at its name unless it is already there.
// The returned records carry the Cloudflare record id, so deleting them
// needs no lookup.
func (c *Cloudflare) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	txts, err := txtRecords(zone, recs)
	if err != nil {
		return nil, err
	}

	added := make([]libdns.Record, 0, len(recs))

	for i, txt := range txts {
		name, value := txt[0], txt[1]
		rr := recs[i].RR()

		zoneID, err := c.zone(ctx, name)
		if err != nil {
			return added, err
		}

		id, err := c.find(ctx, zoneID, name, value)
		if err != nil {
			return added, err
		}

		if id == "" {
			var created cfRecord

			err = c.call(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", cfRecord{
				Type: "TXT", Name: name, Content: value, TTL: max(int(rr.TTL.Seconds()), cloudflareMinTTL),
			}, &created)
			if err != nil {
				return added, err
			}

			id = created.ID
		}

		added = append(added, libdns.TXT{Name: rr.Name, TTL: rr.TTL, Text: value, ProviderData: id})
	}

	return added, nil
}

// DeleteRecords removes each value from its name, by the id
// AppendRecords returned or else by looking the value up. A record that
// is already gone is not an error.
func (c *Cloudflare) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	txts, err := txtRecords(zone, recs)
	if err != nil {
		return nil, err
	}

	deleted := make([]libdns.Record, 0, len(recs))

	for i, txt := range txts {
		name, value := txt[0], txt[1]

		// An empty value would match every record at the name, including
		// ones the operator keeps there.
		if value == "" {
			continue
		}

		zoneID, err := c.zone(ctx, name)
		if err != nil {
			return deleted, err
		}

		id := ""
		if rec, ok := recs[i].(libdns.TXT); ok {
			id, _ = rec.ProviderData.(string)
		}

		if id == "" {
			id, err = c.find(ctx, zoneID, name, value)
			if err != nil {
				return deleted, err
			}
		}

		if id == "" {
			continue
		}

		err = c.call(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+url.PathEscape(id), nil, nil)
		if err != nil && !errors.Is(err, errCloudflareNotFound) {
			return deleted, err
		}

		deleted = append(deleted, recs[i])
	}

	return deleted, nil
}

func (c *Cloudflare) validate() error {
	if c.APIToken == "" {
		return errCloudflareToken
	}

	return nil
}

// find returns the id of the TXT record at name holding value, or "".
func (c *Cloudflare) find(ctx context.Context, zoneID, name, value string) (string, error) {
	var existing []cfRecord

	query := url.Values{"type": {"TXT"}, "name": {name}, "per_page": {"100"}}

	err := c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records?"+query.Encode(), nil, &existing)
	if err != nil {
		return "", err
	}

	for _, record := range existing {
		if strings.Trim(record.Content, `"`) == value {
			return record.ID, nil
		}
	}

	return "", nil
}

// zone is the configured zone, or the longest zone the token can see
// that holds the name.
func (c *Cloudflare) zone(ctx context.Context, name string) (string, error) {
	if c.ZoneID != "" {
		return c.ZoneID, nil
	}

	labels := strings.Split(name, ".")
	for i := 1; i < len(labels); i++ {
		candidate := strings.Join(labels[i:], ".")

		var zones []cfZone

		err := c.call(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(candidate), nil, &zones)
		if err != nil {
			return "", err
		}

		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrZoneNotFound, name)
}

// call performs one API request, decoding the envelope's result into
// out when it is not nil.
func (c *Cloudflare) call(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader = http.NoBody

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding Cloudflare request: %w", err)
		}

		payload = bytes.NewReader(encoded)
	}

	base := c.base
	if base == "" {
		base = CloudflareAPI
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, payload)
	if err != nil {
		return fmt.Errorf("building Cloudflare request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	req.Header.Set("Content-Type", "application/json")

	client := c.client
	if client == nil {
		client = &http.Client{Timeout: cloudflareTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling Cloudflare: %w", err)
	}

	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("reading Cloudflare response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound && method == http.MethodDelete {
		return fmt.Errorf("%w: %s", errCloudflareNotFound, path)
	}

	var envelope cfEnvelope[json.RawMessage]

	decodeErr := json.Unmarshal(raw, &envelope)
	if resp.StatusCode >= http.StatusBadRequest || decodeErr != nil || !envelope.Success {
		return fmt.Errorf("%w: %s %s: %s: %s", ErrCloudflareAPI, method, path, resp.Status,
			cfMessage(envelope.Errors, raw))
	}

	if out != nil {
		err = json.Unmarshal(envelope.Result, out)
		if err != nil {
			return fmt.Errorf("decoding Cloudflare result: %w", err)
		}
	}

	return nil
}

// cfMessage renders the API's errors, or the raw body when it sent none.
func cfMessage(errs []cfError, raw []byte) string {
	if len(errs) == 0 {
		return strings.TrimSpace(string(raw[:min(len(raw), maxErrorBody)]))
	}

	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%d %s", e.Code, e.Message))
	}

	return strings.Join(parts, "; ")
}
