package servertest_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestDERPLatency proves the relay latency report: nodes reporting their network
// measurements home on their preferred relay region, the aggregate report calculates
// preferred counts and latency percentiles (median and p90 in milliseconds),
// and individual node views reflect their preferred relay region.
func TestDERPLatency(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "derp-lat-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	c1 := servertest.NewClient(
		t,
		srv,
		"node1",
		servertest.WithUser(owner),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.NetInfo = &tailcfg.NetInfo{
				PreferredDERP: 1,
				DERPLatency:   map[string]float64{"1-v4": 0.010},
			}
		}),
	)

	c2 := servertest.NewClient(
		t,
		srv,
		"node2",
		servertest.WithUser(owner),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.NetInfo = &tailcfg.NetInfo{
				PreferredDERP: 1,
				DERPLatency:   map[string]float64{"1-v4": 0.030},
			}
		}),
	)

	c1.WaitForPeerCount(t, 1, 10*time.Second)
	c2.WaitForPeerCount(t, 1, 10*time.Second)

	// GET /api/v1/derp/latency shows region 1 preferred by 2 with median and p90.
	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/derp/latency", nil)
	require.Equal(t, http.StatusOK, status, body)

	regions, ok := field(t, body, "regions").([]any)
	require.True(t, ok)

	var region1 map[string]any

	for i := range regions {
		regID := field(t, regions, strconv.Itoa(i), "regionId")
		if regID == float64(1) {
			region1, _ = regions[i].(map[string]any)

			break
		}
	}

	require.NotNil(t, region1, "region 1 should be found in latency report")
	assert.InDelta(t, 2.0, region1["preferredBy"], 0.0)
	assert.InDelta(t, 10.0, region1["medianMs"], 0.1)
	assert.InDelta(t, 30.0, region1["p90Ms"], 0.1)

	// GET /api/v1/node/{id} has netInfo.preferredDerp == 1 for both nodes.
	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+c1.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.InDelta(t, 1.0, field(t, body, "node", "netInfo", "preferredDerp"), 0.0)

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+c2.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.InDelta(t, 1.0, field(t, body, "node", "netInfo", "preferredDerp"), 0.0)
}
