package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// derpFlags mirrors the flags init() registers on the derp subcommands.
func derpFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("url", []string{}, "")
	cmd.Flags().Bool("auto-update", true, "")
	cmd.Flags().String("update-frequency", "3h", "")
	cmd.Flags().Bool("server", true, "")
	cmd.Flags().Int64("region-id", 999, "")
	cmd.Flags().String("region-code", "headscale", "")
	cmd.Flags().String("region-name", "", "")
	cmd.Flags().Bool("verify-clients", true, "")
	cmd.Flags().String("stun", "0.0.0.0:3478", "")
	cmd.Flags().String("ipv4", "", "")
	cmd.Flags().String("ipv6", "", "")

	cmd.Flags().Int64("region", 0, "")
	cmd.Flags().String("code", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("relay-name", "", "")
	cmd.Flags().Int64("derp-port", 0, "")
	cmd.Flags().Int64("stun-port", 0, "")
	cmd.Flags().Bool("stun-only", false, "")
	cmd.Flags().Bool("can-port80", false, "")
}

func sampleDERP() clientv1.DERP {
	regionID := int64(999)
	code := "headscale"
	name := "Headscale Embedded DERP"
	verify := true
	stun := "0.0.0.0:3478"
	ip4 := "198.51.100.1"
	sgpName := "Singapore"
	relay4 := "203.0.113.5"
	nodes := []clientv1.DERPRelay{{HostName: "sgp.derp.example", Ipv4: &relay4}}

	return clientv1.DERP{
		Overridden:     false,
		Paths:          []string{"/etc/headscale/derp.yaml"},
		RelayAvailable: true,
		RelayRunning:   true,
		StunAddr:       "[::]:3478",
		ServerUrl:      "https://headscale.example",
		FetchedAt:      time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		Effective: clientv1.DERPSettings{
			Urls:            []string{"https://controlplane.tailscale.com/derpmap/default"},
			AutoUpdate:      true,
			UpdateFrequency: "3h0m0s",
			Regions:         []clientv1.DERPCustomRegion{{Id: 20, Code: "sgp", Name: &sgpName, Nodes: &nodes}},
			Server: clientv1.DERPServerSettings{
				Enabled:       true,
				RegionId:      &regionID,
				RegionCode:    &code,
				RegionName:    &name,
				VerifyClients: &verify,
				StunAddr:      &stun,
				Ipv4:          &ip4,
			},
		},
		Regions: []clientv1.DERPMapRegion{
			{Id: 1, Code: "nyc", Name: "New York City", Nodes: 4, Source: clientv1.Tailscale},
			{Id: 20, Code: "sgp", Name: "Singapore", Nodes: 1, Source: clientv1.Custom},
			{Id: 999, Code: "headscale", Name: "Headscale Embedded DERP", Nodes: 1, Source: clientv1.Embedded},
		},
	}
}

func TestDERPCommands(t *testing.T) {
	current := sampleDERP()
	overridden := current
	overridden.Overridden = true

	getCurrent := func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
		t.Helper()
		writeJSON(t, w, current)
	}

	cases := []commandCase{
		{
			name:   "show renders source, sources, relay, custom regions and the map",
			src:    showDERPCmd,
			routes: map[string]apiHandler{"GET /api/v1/derp": getCurrent},
			wantIn: []string{
				"Source: config file",
				"Map URLs: https://controlplane.tailscale.com/derpmap/default",
				"Map files: /etc/headscale/derp.yaml",
				"Auto update: every 3h0m0s",
				"Last fetch: 2026-09-08T10:00:00Z",
				"Embedded relay: running at https://headscale.example as region 999 (headscale), STUN on [::]:3478",
				"Verify clients: on",
				"IPv4: 198.51.100.1",
				"Relays you run:",
				"20 sgp (Singapore)",
				"sgp.derp.example (203.0.113.5)",
				"Map: 3 regions",
				"nyc",
				"tailscale",
				"embedded",
			},
		},
		{
			name: "show renders overridden source and a fetch error",
			src:  showDERPCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/derp": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()

					d := overridden
					d.FetchError = "dial tcp: connection refused"
					writeJSON(t, w, d)
				},
			},
			wantIn: []string{
				"Source: set through the API (reset with `headscale derp reset`)",
				"Last fetch error: dial tcp: connection refused",
			},
		},
		{
			name:   "show as json",
			src:    showDERPCmd,
			flags:  map[string]string{"output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/derp": getCurrent},
			want:   indentJSON(t, current),
		},
		{
			name: "set keeps what the flags leave alone",
			src:  setDERPCmd,
			flags: map[string]string{
				"url":    "https://a.example/map,https://b.example/map",
				"server": "false",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/derp": getCurrent,
				"PUT /api/v1/derp": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDERPRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Urls) {
						assert.Equal(t, []string{"https://a.example/map", "https://b.example/map"}, *body.Urls)
					}

					assert.False(t, body.Server.Enabled)

					if assert.NotNil(t, body.Server.RegionId) {
						assert.Equal(t, int64(999), *body.Server.RegionId, "untouched relay fields are kept")
					}

					if assert.NotNil(t, body.AutoUpdate) {
						assert.True(t, *body.AutoUpdate)
					}

					if assert.NotNil(t, body.Regions) {
						assert.Len(t, *body.Regions, 1, "custom regions are kept")
					}

					writeJSON(t, w, overridden)
				},
			},
			wantIn: []string{"Source: set through the API"},
		},
		{
			name:  "set changes the relay fields",
			src:   setDERPCmd,
			flags: map[string]string{"region-id": "950", "stun": "0.0.0.0:3479", "verify-clients": "false"},
			routes: map[string]apiHandler{
				"GET /api/v1/derp": getCurrent,
				"PUT /api/v1/derp": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDERPRequestBody

					decodeBody(t, r, &body)

					require.NotNil(t, body.Server.RegionId)
					assert.Equal(t, int64(950), *body.Server.RegionId)
					require.NotNil(t, body.Server.StunAddr)
					assert.Equal(t, "0.0.0.0:3479", *body.Server.StunAddr)
					require.NotNil(t, body.Server.VerifyClients)
					assert.False(t, *body.Server.VerifyClients)

					writeJSON(t, w, overridden)
				},
			},
			wantIn: []string{"Source: set through the API"},
		},
		{
			name: "set reports an API error",
			src:  setDERPCmd,
			flags: map[string]string{
				"update-frequency": "10s",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/derp": getCurrent,
				"PUT /api/v1/derp": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(
						t,
						w,
						http.StatusBadRequest,
						"the map cannot be refetched more often than every minute",
					)
				},
			},
			wantErr: "refetched more often",
		},
		{
			name: "reset",
			src:  resetDERPCmd,
			routes: map[string]apiHandler{
				"DELETE /api/v1/derp": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
			},
			wantIn: []string{"DERP settings reset to the config file", "Source: config file"},
		},
		{
			name: "refresh",
			src:  refreshDERPCmd,
			routes: map[string]apiHandler{
				"POST /api/v1/derp/refresh": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
			},
			wantIn: []string{"DERP map refreshed"},
		},
		{
			name: "relay add creates a region and keeps the rest",
			src:  addDERPRelayCmd,
			flags: map[string]string{
				"region": "30", "code": "fra", "host": "fra.derp.example", "derp-port": "8443", "can-port80": "true",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/derp": getCurrent,
				"PUT /api/v1/derp": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDERPRequestBody

					decodeBody(t, r, &body)
					require.NotNil(t, body.Regions)
					require.Len(t, *body.Regions, 2)

					fra := (*body.Regions)[1]
					assert.Equal(t, int64(30), fra.Id)
					assert.Equal(t, "fra", fra.Code)
					require.NotNil(t, fra.Nodes)
					require.Len(t, *fra.Nodes, 1)
					assert.Equal(t, "fra.derp.example", (*fra.Nodes)[0].HostName)
					require.NotNil(t, (*fra.Nodes)[0].DerpPort)
					assert.Equal(t, int64(8443), *(*fra.Nodes)[0].DerpPort)
					require.NotNil(t, (*fra.Nodes)[0].CanPort80)
					assert.True(t, *(*fra.Nodes)[0].CanPort80)
					assert.True(t, body.Server.Enabled, "the relay settings are kept")

					writeJSON(t, w, overridden)
				},
			},
			wantIn: []string{"Relay fra.derp.example added to region 30"},
		},
		{
			name:  "relay add replaces a relay with the same host in an existing region",
			src:   addDERPRelayCmd,
			flags: map[string]string{"region": "20", "host": "sgp.derp.example", "ipv4": "203.0.113.9"},
			routes: map[string]apiHandler{
				"GET /api/v1/derp": getCurrent,
				"PUT /api/v1/derp": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDERPRequestBody

					decodeBody(t, r, &body)
					require.NotNil(t, body.Regions)
					require.Len(t, *body.Regions, 1)

					sgp := (*body.Regions)[0]
					assert.Equal(t, "sgp", sgp.Code, "the region's code is kept")
					require.NotNil(t, sgp.Nodes)
					require.Len(t, *sgp.Nodes, 1)
					require.NotNil(t, (*sgp.Nodes)[0].Ipv4)
					assert.Equal(t, "203.0.113.9", *(*sgp.Nodes)[0].Ipv4)

					writeJSON(t, w, overridden)
				},
			},
			wantIn: []string{"Relay sgp.derp.example added to region 20"},
		},
		{
			name:  "relay remove drops the region when its last relay goes",
			src:   removeDERPRelayCmd,
			flags: map[string]string{"region": "20", "host": "sgp.derp.example"},
			routes: map[string]apiHandler{
				"GET /api/v1/derp": getCurrent,
				"PUT /api/v1/derp": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDERPRequestBody

					decodeBody(t, r, &body)
					require.NotNil(t, body.Regions)
					assert.Empty(t, *body.Regions)

					writeJSON(t, w, overridden)
				},
			},
			wantIn: []string{"Relay sgp.derp.example removed from region 20"},
		},
		{
			name:  "relay remove of a whole region",
			src:   removeDERPRelayCmd,
			flags: map[string]string{"region": "20"},
			routes: map[string]apiHandler{
				"GET /api/v1/derp": getCurrent,
				"PUT /api/v1/derp": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDERPRequestBody

					decodeBody(t, r, &body)
					require.NotNil(t, body.Regions)
					assert.Empty(t, *body.Regions)

					writeJSON(t, w, overridden)
				},
			},
			wantIn: []string{"Region 20 removed"},
		},
		{
			name:    "relay remove of an unknown region",
			src:     removeDERPRelayCmd,
			flags:   map[string]string{"region": "77"},
			routes:  map[string]apiHandler{"GET /api/v1/derp": getCurrent},
			wantErr: "no such relay region",
		},
		{
			name:    "relay remove of an unknown host",
			src:     removeDERPRelayCmd,
			flags:   map[string]string{"region": "20", "host": "nope.derp.example"},
			routes:  map[string]apiHandler{"GET /api/v1/derp": getCurrent},
			wantErr: "no such relay in the region",
		},
	}

	runCommandCases(t, derpFlags, cases)
}
