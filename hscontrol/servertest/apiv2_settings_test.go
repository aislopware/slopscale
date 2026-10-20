package servertest_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tsclient "tailscale.com/client/tailscale/v2"
)

// TestAPIv2TailnetSettingsWrite writes the tailnet settings through the two
// clients operators use for it, the Tailscale Go client and the Terraform
// provider, against a server on a real loopback port. The Go client sends
// aclsExternallyManagedOn and aclsExternalLink as null on every update, so
// before the server took them every write it made was refused.
func TestAPIv2TailnetSettingsWrite(t *testing.T) {
	srv := servertest.NewServer(t, servertest.WithRealListener())
	owner := srv.CreateUser(t, "apiv2-settings")
	apiKey := srv.CreateAPIKey(t, owner)

	t.Run("GoClient", func(t *testing.T) {
		ctx := t.Context()
		settings := goClient(t, srv.URL, apiKey).TailnetSettings()

		require.NoError(t, settings.Update(ctx, tsclient.UpdateTailnetSettingsRequest{
			DevicesApprovalOn: new(true),
		}))
		assert.True(t, srv.State().Settings().DevicesApprovalOn)

		got, err := settings.Get(ctx)
		require.NoError(t, err)
		assert.True(t, got.DevicesApprovalOn)

		// The route selection is a real setting.
		require.NoError(t, settings.Update(ctx, tsclient.UpdateTailnetSettingsRequest{
			RouteSelection: new(tsclient.RouteSelectionActivePassiveFailover),
		}))
		assert.Equal(t, types.RouteSelectionActivePassive, srv.State().RouteSelection())

		got, err = settings.Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, tsclient.RouteSelectionActivePassiveFailover, got.RouteSelection)
		assert.False(t, got.RegionalRoutingOn) //nolint:staticcheck // the legacy field must follow

		require.NoError(t, settings.Update(ctx, tsclient.UpdateTailnetSettingsRequest{
			RouteSelection: new(tsclient.RouteSelectionRegionalRouting),
		}))

		// The hosted API refuses both route selection fields at once.
		//nolint:staticcheck // the deprecated field is what Terraform still sends
		err = settings.Update(ctx, tsclient.UpdateTailnetSettingsRequest{
			RegionalRoutingOn: new(true),
			RouteSelection:    new(tsclient.RouteSelectionRegionalRouting),
		})
		requireStatus(t, err, http.StatusBadRequest)

		// A setting Slopscale cannot change is refused, and the request
		// changes nothing else.
		err = settings.Update(ctx, tsclient.UpdateTailnetSettingsRequest{
			DevicesApprovalOn:    new(false),
			DevicesAutoUpdatesOn: new(true),
		})
		requireStatus(t, err, http.StatusBadRequest)
		assert.Contains(t, err.Error(), "devicesAutoUpdatesOn")
		assert.True(t, srv.State().Settings().DevicesApprovalOn)

		require.NoError(t, settings.Update(ctx, tsclient.UpdateTailnetSettingsRequest{
			DevicesApprovalOn: new(false),
		}))
	})

	t.Run("Terraform", func(t *testing.T) {
		tf := newTofu(t, srv.URL, apiKey, fmt.Sprintf(tailnetSettingsTFConfig, true))

		tf.run("init", "-no-color", "-input=false")
		tf.run("apply", "-auto-approve", "-no-color", "-input=false")

		current := srv.State().Settings()
		assert.True(t, current.UsersApprovalOn)
		assert.False(t, current.DevicesApprovalOn)
		assert.Equal(t, types.RouteSelectionRegional, srv.State().RouteSelection(),
			"regional_routing_on = true matches the default, so nothing drifts")

		tf.assertNoDrift()

		// regional_routing_on = false is active-passive failover.
		require.NoError(t, os.WriteFile(filepath.Join(tf.cmd().Dir, "main.tf"),
			[]byte(fmt.Sprintf(tailnetSettingsTFConfig, false)), 0o600))
		tf.run("apply", "-auto-approve", "-no-color", "-input=false")
		assert.Equal(t, types.RouteSelectionActivePassive, srv.State().RouteSelection())
		tf.assertNoDrift()

		tf.run("destroy", "-auto-approve", "-no-color", "-input=false")
	})
}

// tailnetSettingsTFConfig sets the switches the API changes and, through
// %t, regional_routing_on, as a configuration written for the hosted
// control plane would.
const tailnetSettingsTFConfig = `
terraform {
  required_providers {
    tailscale = {
      source  = "tailscale/tailscale"
      version = "~> 0.21"
    }
  }
}

provider "tailscale" {}

resource "tailscale_tailnet_settings" "s" {
  devices_approval_on = false
  users_approval_on   = true
  regional_routing_on = %t
}
`

// requireStatus fails unless err is the Tailscale client's error with the
// given HTTP status.
func requireStatus(t *testing.T, err error, status int) {
	t.Helper()

	var apiErr tsclient.APIError

	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, status, apiErr.Status)
}
