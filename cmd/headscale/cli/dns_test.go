package cli

import (
	"net/http"
	"testing"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dnsFlags mirrors the flags init() registers on the dns subcommands.
func dnsFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("nameserver", []string{}, "")
	cmd.Flags().Bool("override-local-dns", false, "")
	cmd.Flags().StringSlice("split", []string{}, "")
	cmd.Flags().StringSlice("search-domain", []string{}, "")
	cmd.Flags().StringArray("record", []string{}, "")
	cmd.Flags().Bool("keep", false, "")
}

func sampleDNS() clientv1.DNS {
	splitNS := []string{"10.0.0.1"}

	return clientv1.DNS{
		MagicDns:         true,
		BaseDomain:       "example.com",
		Overridden:       false,
		ExtraRecordsPath: "/var/lib/headscale/records",
		Effective: clientv1.DNSSettings{
			Nameservers:      []string{"1.1.1.1", "8.8.8.8"},
			OverrideLocalDns: true,
			SplitNameservers: map[string]*[]string{
				"corp.example": &splitNS,
			},
			SearchDomains: []string{"search.example"},
			ExtraRecords: []clientv1.DNSRecord{
				{
					Name:  "myhost",
					Type:  clientv1.A,
					Value: "1.2.3.4",
				},
			},
		},
		FromFile: clientv1.DNSSettings{},
	}
}

func TestDNSCommands(t *testing.T) {
	current := sampleDNS()
	overridden := current
	overridden.Overridden = true

	cases := []commandCase{
		{
			name: "show renders MagicDNS base domain source nameservers split search extra",
			src:  showDNSCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
			},
			wantIn: []string{
				"MagicDNS: on",
				"Base domain: example.com",
				"Source: config file",
				"Extra records: read from /var/lib/headscale/records",
				"Nameservers: 1.1.1.1, 8.8.8.8",
				"Override local DNS: on",
				"corp.example",
				"10.0.0.1",
				"Search domains: search.example",
				"myhost",
				"1.2.3.4",
			},
		},
		{
			name: "show renders overridden source",
			src:  showDNSCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, overridden)
				},
			},
			wantIn: []string{
				"Source: set through the API (reset with `headscale dns reset`)",
			},
		},
		{
			name:  "show as json",
			src:   showDNSCmd,
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
			},
			want: indentJSON(t, current),
		},
		{
			name: "set builds the right body with repeat and semicolon split and typed records",
			src:  setDNSCmd,
			flags: map[string]string{
				"nameserver":         "1.1.1.1,8.8.8.8",
				"override-local-dns": "true",
				"split":              "corp.example=10.0.0.1,corp.example=10.0.0.2,other.example=10.0.0.3;10.0.0.4",
				"search-domain":      "search.example",
				"record":             "plain=1.2.3.4\ntyped=A:5.6.7.8",
			},
			routes: map[string]apiHandler{
				"PUT /api/v1/dns": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDNSRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Nameservers) {
						assert.Equal(t, []string{"1.1.1.1", "8.8.8.8"}, *body.Nameservers)
					}

					if assert.NotNil(t, body.OverrideLocalDns) {
						assert.True(t, *body.OverrideLocalDns)
					}

					if assert.NotNil(t, body.SearchDomains) {
						assert.Equal(t, []string{"search.example"}, *body.SearchDomains)
					}

					if assert.NotNil(t, body.SplitNameservers) {
						split := *body.SplitNameservers
						if assert.Contains(t, split, "corp.example") && assert.NotNil(t, split["corp.example"]) {
							assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, *split["corp.example"])
						}

						if assert.Contains(t, split, "other.example") && assert.NotNil(t, split["other.example"]) {
							assert.Equal(t, []string{"10.0.0.3", "10.0.0.4"}, *split["other.example"])
						}
					}

					if assert.NotNil(t, body.ExtraRecords) {
						records := *body.ExtraRecords
						assert.Len(t, records, 2)
						assert.Equal(t, "plain", records[0].Name)
						assert.Equal(t, clientv1.Empty, records[0].Type)
						assert.Equal(t, "1.2.3.4", records[0].Value)
						assert.Equal(t, "typed", records[1].Name)
						assert.Equal(t, clientv1.A, records[1].Type)
						assert.Equal(t, "5.6.7.8", records[1].Value)
					}

					writeJSON(t, w, current)
				},
			},
			wantIn: []string{"MagicDNS: on"},
		},
		{
			name: "set --keep keeps unchanged fields and replaces given field",
			src:  setDNSCmd,
			flags: map[string]string{
				"keep":       "true",
				"nameserver": "9.9.9.9",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
				"PUT /api/v1/dns": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetDNSRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Nameservers) {
						assert.Equal(t, []string{"9.9.9.9"}, *body.Nameservers)
					}

					if assert.NotNil(t, body.OverrideLocalDns) {
						assert.Equal(t, current.Effective.OverrideLocalDns, *body.OverrideLocalDns)
					}

					if assert.NotNil(t, body.SearchDomains) {
						assert.Equal(t, current.Effective.SearchDomains, *body.SearchDomains)
					}

					if assert.NotNil(t, body.SplitNameservers) {
						assert.Equal(t, current.Effective.SplitNameservers, *body.SplitNameservers)
					}

					if assert.NotNil(t, body.ExtraRecords) {
						assert.Equal(t, current.Effective.ExtraRecords, *body.ExtraRecords)
					}

					writeJSON(t, w, current)
				},
			},
			wantIn: []string{"MagicDNS: on"},
		},
		{
			name:    "set with malformed split entry fails and makes no request",
			src:     setDNSCmd,
			flags:   map[string]string{"split": "malformed"},
			routes:  map[string]apiHandler{},
			wantErr: "invalid DNS flag format",
		},
		{
			name:    "set with malformed record entry fails and makes no request",
			src:     setDNSCmd,
			flags:   map[string]string{"record": "malformed"},
			routes:  map[string]apiHandler{},
			wantErr: "invalid DNS flag format",
		},
		{
			name: "reset calls DELETE and prints confirmation",
			src:  resetDNSCmd,
			routes: map[string]apiHandler{
				"DELETE /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
			},
			wantIn: []string{
				"DNS settings reset to the config file",
				"MagicDNS: on",
			},
		},
		{
			name: "get surfaces api error",
			src:  showDNSCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "missing scope")
				},
			},
			wantErr: "missing scope",
		},
		{
			name:  "set surfaces api error",
			src:   setDNSCmd,
			flags: map[string]string{"nameserver": "1.1.1.1"},
			routes: map[string]apiHandler{
				"PUT /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "invalid nameserver IP")
				},
			},
			wantErr: "invalid nameserver IP",
		},
		{
			name: "reset surfaces api error",
			src:  resetDNSCmd,
			routes: map[string]apiHandler{
				"DELETE /api/v1/dns": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "reset failed")
				},
			},
			wantErr: "reset failed",
		},
	}

	runCommandCases(t, dnsFlags, cases)
}

func TestDNSSetRepeatFlags(t *testing.T) {
	current := sampleDNS()

	serveAPI(t, map[string]apiHandler{
		"PUT /api/v1/dns": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			t.Helper()

			var body clientv1.SetDNSRequestBody

			decodeBody(t, r, &body)

			if assert.NotNil(t, body.SplitNameservers) {
				split := *body.SplitNameservers
				assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, *split["corp.example"])
			}

			if assert.NotNil(t, body.ExtraRecords) {
				records := *body.ExtraRecords
				assert.Len(t, records, 2)
				assert.Equal(t, "plain", records[0].Name)
				assert.Equal(t, clientv1.Empty, records[0].Type)
				assert.Equal(t, "typed", records[1].Name)
				assert.Equal(t, clientv1.A, records[1].Type)
			}

			writeJSON(t, w, current)
		},
	})

	cmd := newTestCommand(t, setDNSCmd, dnsFlags, nil)
	require.NoError(t, cmd.Flags().Set("split", "corp.example=10.0.0.1"))
	require.NoError(t, cmd.Flags().Set("split", "corp.example=10.0.0.2"))
	require.NoError(t, cmd.Flags().Set("record", "plain=1.2.3.4"))
	require.NoError(t, cmd.Flags().Set("record", "typed=A:5.6.7.8"))

	out, err := runCommand(t, cmd)
	require.NoError(t, err)
	assert.Contains(t, out, "MagicDNS: on")
}
