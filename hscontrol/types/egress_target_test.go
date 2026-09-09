package types

import (
	"testing"

	"github.com/juanfont/headscale/hscontrol/egress"
	"github.com/stretchr/testify/require"
)

// TestValidatorsRefuseBlockedTargets proves each operator-supplied URL is
// checked against the egress guard when it is stored, and that the guard's
// sentinel survives, because the API maps it to a 400.
//
// The guard's default policy is process-wide, so these cases do not run in
// parallel with each other.
func TestValidatorsRefuseBlockedTargets(t *testing.T) {
	egress.SetDefault(egress.Policy{})
	t.Cleanup(func() { egress.SetDefault(egress.Policy{}) })

	t.Run("webhook", func(t *testing.T) {
		blocked := Webhook{
			ProviderType:  WebhookProviderGeneric,
			URL:           "http://169.254.169.254/latest/meta-data/",
			Subscriptions: []WebhookEventType{EventNodeCreated},
		}

		err := ValidateWebhook(blocked)
		require.ErrorIs(t, err, egress.ErrBlocked)
		require.Contains(t, err.Error(), "webhook URL points at a blocked address (link-local)")

		blocked.URL = "http://127.0.0.1:9000/hook"
		require.ErrorIs(t, ValidateWebhook(blocked), egress.ErrBlocked)

		blocked.URL = "http://example.com/hook"
		require.NoError(t, ValidateWebhook(blocked))
	})

	t.Run("log stream", func(t *testing.T) {
		blocked := LogStream{
			Name:        "sink",
			Destination: LogStreamSplunk,
			URL:         "http://[::1]:8088/services/collector",
			Token:       "t",
		}

		err := ValidateLogStream(blocked)
		require.ErrorIs(t, err, egress.ErrBlocked)
		require.Contains(t, err.Error(), "log stream URL points at a blocked address (loopback)")

		blocked.URL = "https://splunk.example.com/services/collector"
		require.NoError(t, ValidateLogStream(blocked))
	})

	t.Run("derp map url", func(t *testing.T) {
		// An IPv6 literal in a URL host is bracketed, and the IPv4-mapped
		// form reaches the same host as 127.0.0.1.
		blocked := DERPSettings{URLs: []string{"http://[::ffff:127.0.0.1]/derpmap"}}

		err := blocked.Validate()
		require.ErrorIs(t, err, egress.ErrBlocked)
		require.Contains(t, err.Error(), "DERP map URL points at a blocked address (loopback)")

		blocked.URLs = []string{"https://controlplane.tailscale.com/derpmap/default"}
		require.NoError(t, blocked.Validate())
	})

	t.Run("loopback allowed by config", func(t *testing.T) {
		egress.SetDefault(egress.Policy{AllowLoopback: true})
		t.Cleanup(func() { egress.SetDefault(egress.Policy{}) })

		allowed := Webhook{
			ProviderType:  WebhookProviderGeneric,
			URL:           "http://127.0.0.1:9000/hook",
			Subscriptions: []WebhookEventType{EventNodeCreated},
		}

		require.NoError(t, ValidateWebhook(allowed))
	})
}
