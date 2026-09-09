// Package integration asks device management and endpoint security
// services what they know about a machine, by serial number, and turns
// the answer into posture attributes. One [Client] per provider; the
// attribute names are the ones Tailscale's device posture integrations
// document, so a posture written for Tailscale works unchanged.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// Attributes are what a provider knows about one machine, keyed without
// the provider's prefix: ztaScore, complianceState, mdmEnabled.
type Attributes map[string]any

// Client looks machines up at one provider.
type Client interface {
	// Lookup returns the attributes of every serial the provider knows;
	// a serial it does not know is absent from the map.
	Lookup(ctx context.Context, serials []string) (map[string]Attributes, error)
	// Check verifies the credentials reach the provider.
	Check(ctx context.Context) error
}

// Errors a lookup can fail with; the sync records them on the
// integration.
var (
	// ErrAuth is a rejected credential; the sync keeps trying at the
	// next tick, as Tailscale does, and the operator sees it.
	ErrAuth = errors.New("the provider rejected the credentials")
	// ErrUpstream is any other failed request.
	ErrUpstream = errors.New("the provider request failed")
	// ErrProvider is an unknown provider.
	ErrProvider = errors.New("unknown posture provider")
)

// maxResponse bounds what is read from a provider.
const maxResponse = 8 << 20

// defaultTimeout bounds a request when the caller brings no client.
const defaultTimeout = 30 * time.Second

// defaultTokenTTL is assumed when a token response says nothing.
const defaultTokenTTL = 30 * time.Minute

// authorization is the header every provider takes its credential in.
const authorization = "Authorization"

// New returns the client for an integration.
//
//nolint:ireturn // one client type per provider, chosen by the config
func New(i types.PostureIntegration, httpClient *http.Client) (Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	c := &httpDoer{client: httpClient}

	switch i.Provider {
	case types.PostureProviderFalcon:
		return &falcon{httpDoer: c, cfg: i.Config}, nil
	case types.PostureProviderSentinelOne:
		return &sentinelOne{httpDoer: c, cfg: i.Config}, nil
	case types.PostureProviderIntune:
		return &intune{httpDoer: c, cfg: i.Config}, nil
	case types.PostureProviderJamf:
		return &jamf{httpDoer: c, cfg: i.Config}, nil
	case types.PostureProviderKandji:
		return &kandji{httpDoer: c, cfg: i.Config}, nil
	case types.PostureProviderKolide:
		return &kolide{httpDoer: c, cfg: i.Config}, nil
	}

	return nil, fmt.Errorf("%w: %q", ErrProvider, i.Provider)
}

// httpDoer is the shared request helper.
type httpDoer struct {
	client *http.Client
}

// request is one call to the provider.
type request struct {
	method  string
	url     string
	headers map[string]string
	// form, when set, is sent URL-encoded; body is sent as is.
	form url.Values
	body []byte
}

// doJSON performs the request and decodes a JSON body into dest. A 401
// or 403 is [ErrAuth]; any other non-2xx status is [ErrUpstream] with
// the status and the start of the body.
func (d *httpDoer) doJSON(ctx context.Context, r request, dest any) error {
	var body io.Reader

	if r.form != nil {
		body = strings.NewReader(r.form.Encode())
	} else if r.body != nil {
		body = strings.NewReader(string(r.body))
	}

	req, err := http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpstream, err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "slopscale-posture")

	if r.form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("%w: reading response: %w", ErrUpstream, err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrAuth, resp.Status)
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		return fmt.Errorf("%w: %s: %s", ErrUpstream, resp.Status, snippet(data))
	}

	if dest == nil || len(data) == 0 {
		return nil
	}

	err = json.Unmarshal(data, dest)
	if err != nil {
		return fmt.Errorf("%w: decoding response: %w", ErrUpstream, err)
	}

	return nil
}

// snippet keeps the start of an error body for the log.
// snippetLength is how much of an error body the error keeps.
const snippetLength = 200

func snippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	if len(s) > snippetLength {
		s = s[:snippetLength] + "…"
	}

	return s
}

// bearerToken is an OAuth token with its expiry, refreshed a minute
// early.
type bearerToken struct {
	value   string
	expires time.Time
}

func (t bearerToken) valid(now time.Time) bool {
	return t.value != "" && now.Add(time.Minute).Before(t.expires)
}

// tokenResponse is the OAuth client-credentials answer every provider
// with an OAuth client returns.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (r tokenResponse) token(now time.Time) (bearerToken, error) {
	if r.AccessToken == "" {
		return bearerToken{}, fmt.Errorf("%w: token response carried no access_token", ErrUpstream)
	}

	ttl := time.Duration(r.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}

	return bearerToken{value: r.AccessToken, expires: now.Add(ttl)}, nil
}

// batches splits serials into groups of n, the largest a provider takes
// per request.
func batches(serials []string, n int) [][]string {
	var out [][]string

	for len(serials) > 0 {
		k := min(n, len(serials))
		out = append(out, serials[:k])
		serials = serials[k:]
	}

	return out
}

// strip drops characters that would break a provider's filter syntax;
// serial numbers are alphanumeric with dashes in practice.
func strip(serial string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return -1
		}
	}, serial)
}
