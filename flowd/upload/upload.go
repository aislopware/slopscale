// Package upload delivers spooled reports to the server, oldest first,
// authenticated by the gateway node's identity token.
package upload

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/flowd/spool"
	"github.com/aislopware/slopscale/hscontrol/traffic"
)

const (
	requestTimeout  = 30 * time.Second
	minBackoff      = 5 * time.Second
	maxBackoff      = 5 * time.Minute
	refusedBackoff  = 5 * time.Minute
	tokenMargin     = time.Minute
	maxResponseSize = 1 << 20
	jwtParts        = 3
)

var (
	// ErrUnauthorized is returned when the server refuses the token even
	// after a fresh one was fetched.
	ErrUnauthorized = errors.New("server refused the identity token")
	// ErrForbidden is returned when the server refuses the gateway: the
	// node is not an exit node, subnet router or app connector, or the
	// feature is off.
	ErrForbidden    = errors.New("server refused the gateway")
	errServerStatus = errors.New("server answered with an error")
)

// TokenFetcher returns a fresh identity token for [traffic.Audience].
type TokenFetcher func(ctx context.Context) (string, error)

// Tokens caches the identity token until shortly before it expires.
type Tokens struct {
	fetch   TokenFetcher
	now     func() time.Time
	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewTokens returns a cache over fetch.
func NewTokens(fetch TokenFetcher) *Tokens {
	return &Tokens{fetch: fetch, now: time.Now}
}

// Token returns a cached token or fetches one.
func (t *Tokens) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.token != "" && t.now().Add(tokenMargin).Before(t.expires) {
		return t.token, nil
	}

	token, err := t.fetch(ctx)
	if err != nil {
		return "", fmt.Errorf("fetching an identity token: %w", err)
	}

	t.token = token
	t.expires = tokenExpiry(token, t.now())

	return token, nil
}

// Invalidate drops the cached token, after the server refused it.
func (t *Tokens) Invalidate() {
	t.mu.Lock()
	t.token = ""
	t.mu.Unlock()
}

// tokenExpiry reads the exp claim without verifying the token, which is
// the server's job; an unreadable one is treated as good for a minute past
// the margin.
func tokenExpiry(token string, now time.Time) time.Time {
	fallback := now.Add(2 * tokenMargin)

	parts := strings.Split(token, ".")
	if len(parts) != jwtParts {
		return fallback
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}

	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return fallback
	}

	return time.Unix(claims.Exp, 0)
}

// Uploader sends the spool's reports to the server.
type Uploader struct {
	url      string
	client   *http.Client
	tokens   *Tokens
	spool    *spool.Spool
	onConfig func(traffic.Config)
	log      *slog.Logger
	kick     chan struct{}
	sleep    func(ctx context.Context, d time.Duration)
}

// New returns an uploader posting to server's [traffic.ReportPath].
func New(
	server string,
	client *http.Client,
	tokens *Tokens,
	sp *spool.Spool,
	onConfig func(traffic.Config),
	logger *slog.Logger,
) *Uploader {
	return &Uploader{
		url:      strings.TrimSuffix(server, "/") + traffic.ReportPath,
		client:   client,
		tokens:   tokens,
		spool:    sp,
		onConfig: onConfig,
		log:      logger,
		kick:     make(chan struct{}, 1),
		sleep:    sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Kick asks the uploader to send what is spooled now.
func (u *Uploader) Kick() {
	select {
	case u.kick <- struct{}{}:
	default:
	}
}

// Run sends spooled reports until ctx ends, waking on [Uploader.Kick].
func (u *Uploader) Run(ctx context.Context) {
	backoff := minBackoff

	for {
		wait, err := u.Drain(ctx)

		switch {
		case err == nil:
			backoff = minBackoff
		case wait > 0:
			u.log.Warn("the server refused the report; retrying later", "err", err, "retry_in", wait)
			u.sleep(ctx, wait)

			continue
		default:
			u.log.Warn("delivering reports failed; retrying", "err", err, "retry_in", backoff)
			u.sleep(ctx, backoff)
			backoff = min(backoff*2, maxBackoff)

			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-u.kick:
		}
	}
}

// Drain sends every spooled report, oldest first. On failure it returns
// the error and, when the server refused rather than failed, how long to
// wait before trying again.
func (u *Uploader) Drain(ctx context.Context) (time.Duration, error) {
	for ctx.Err() == nil {
		seq, body, err := u.spool.Oldest()
		if errors.Is(err, spool.ErrEmpty) {
			return 0, nil
		}

		if err != nil {
			return 0, err
		}

		resp, wait, err := u.send(ctx, seq, body)
		if err != nil {
			return wait, err
		}

		if resp == nil {
			continue // rejected for good and dropped
		}

		err = u.spool.Ack(max(resp.Seq, seq))
		if err != nil {
			return 0, err
		}

		if u.onConfig != nil {
			u.onConfig(resp.Config)
		}
	}

	return 0, ctx.Err()
}

// send posts one report, refreshing the token once on 401. A report the
// server rejects as malformed or too large can never succeed, so it is
// dropped from the spool.
func (u *Uploader) send(ctx context.Context, seq uint64, body []byte) (*traffic.Response, time.Duration, error) {
	for attempt := range 2 {
		token, err := u.tokens.Token(ctx)
		if err != nil {
			return nil, 0, err
		}

		status, raw, err := u.post(ctx, token, body)
		if err != nil {
			return nil, 0, err
		}

		switch {
		case status == http.StatusOK:
			var resp traffic.Response

			err = json.Unmarshal(raw, &resp)
			if err != nil {
				return nil, 0, fmt.Errorf("decoding the server's response: %w", err)
			}

			return &resp, 0, nil
		case status == http.StatusUnauthorized && attempt == 0:
			u.tokens.Invalidate()

			continue
		case status == http.StatusUnauthorized:
			return nil, refusedBackoff, ErrUnauthorized
		case status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge:
			u.log.Error("the server rejected a report; dropping it", "seq", seq, "status", status, "body", string(raw))

			return nil, 0, u.spool.Reject(seq)
		case status == http.StatusForbidden:
			return nil, refusedBackoff, fmt.Errorf("%w: %s", ErrForbidden, strings.TrimSpace(string(raw)))
		default:
			return nil, 0, fmt.Errorf("%w: %d: %s", errServerStatus, status, strings.TrimSpace(string(raw)))
		}
	}

	return nil, refusedBackoff, ErrUnauthorized
}

func (u *Uploader) post(ctx context.Context, token string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("building the request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")

	resp, err := u.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("posting the report: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return 0, nil, fmt.Errorf("reading the response: %w", err)
	}

	return resp.StatusCode, raw, nil
}
