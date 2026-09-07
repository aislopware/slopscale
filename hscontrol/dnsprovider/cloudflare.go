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

	"github.com/juanfont/headscale/hscontrol/types"
)

const (
	// CloudflareAPI is the API base; tests point it elsewhere.
	CloudflareAPI = "https://api.cloudflare.com/client/v4"

	cloudflareTimeout = 30 * time.Second
	// maxErrorBody bounds what an error response contributes to the
	// error text.
	maxErrorBody = 4 << 10
)

var (
	// ErrZoneNotFound is returned when no zone holds the record's name.
	ErrZoneNotFound = errors.New("no Cloudflare zone holds the record name")
	// ErrCloudflareAPI wraps an answer the API refused.
	ErrCloudflareAPI = errors.New("cloudflare API error")
)

// Cloudflare publishes through the Cloudflare API with a scoped token.
type Cloudflare struct {
	token  string
	zoneID string
	ttl    time.Duration
	base   string
	client *http.Client
}

// NewCloudflare builds the provider.
func NewCloudflare(cfg types.CloudflareDNSConfig, ttl time.Duration) *Cloudflare {
	return &Cloudflare{
		token:  cfg.APIToken,
		zoneID: cfg.ZoneID,
		ttl:    ttl,
		base:   CloudflareAPI,
		client: &http.Client{Timeout: cloudflareTimeout},
	}
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
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
}

// SetTXT adds the value at the name unless it is already there.
func (c *Cloudflare) SetTXT(ctx context.Context, name, value string) error {
	name = strings.TrimSuffix(strings.ToLower(name), ".")

	zoneID, err := c.zone(ctx, name)
	if err != nil {
		return err
	}

	var existing []cfRecord

	query := url.Values{"type": {"TXT"}, "name": {name}, "per_page": {"100"}}

	err = c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records?"+query.Encode(), nil, &existing)
	if err != nil {
		return err
	}

	for _, record := range existing {
		if strings.Trim(record.Content, `"`) == value {
			return nil
		}
	}

	record := cfRecord{Type: "TXT", Name: name, Content: value, TTL: int(c.ttl.Seconds())}

	return c.call(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", record, nil)
}

// zone is the configured zone, or the longest zone the token can see
// that holds the name.
func (c *Cloudflare) zone(ctx context.Context, name string) (string, error) {
	if c.zoneID != "" {
		return c.zoneID, nil
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

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, payload)
	if err != nil {
		return fmt.Errorf("building Cloudflare request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("calling Cloudflare: %w", err)
	}

	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return fmt.Errorf("reading Cloudflare response: %w", err)
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
		return strings.TrimSpace(string(raw))
	}

	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%d %s", e.Code, e.Message))
	}

	return strings.Join(parts, "; ")
}
