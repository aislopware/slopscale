package clientversion_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/clientversion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

func TestLatest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stable/":
			_, _ = w.Write([]byte(`{"Version":"1.86.2","Tarballs":{"amd64":"tailscale_1.86.2_amd64.tgz"}}`))
		case "/empty/":
			_, _ = w.Write([]byte(`{"Version":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	version, err := clientversion.Latest(t.Context(), srv.Client(), srv.URL+"/stable/?mode=json")
	require.NoError(t, err)
	assert.Equal(t, "1.86.2", version)

	_, err = clientversion.Latest(t.Context(), srv.Client(), srv.URL+"/empty/")
	require.ErrorIs(t, err, clientversion.ErrNoVersion)

	_, err = clientversion.Latest(t.Context(), srv.Client(), srv.URL+"/missing/")
	require.ErrorIs(t, err, clientversion.ErrUnexpectedStatus)
}

func TestFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		running string
		latest  string
		want    *tailcfg.ClientVersion
	}{
		{"latest unknown", "1.86.2-t1a2b", "", nil},
		{"version unknown", "", "1.86.2", nil},
		{"behind", "1.84.0-t1a2b-g3c4d", "1.86.2", &tailcfg.ClientVersion{LatestVersion: "1.86.2"}},
		{"behind on patch", "1.86.1", "1.86.2", &tailcfg.ClientVersion{LatestVersion: "1.86.2"}},
		{"running latest", "1.86.2-t1a2b", "1.86.2", &tailcfg.ClientVersion{RunningLatest: true}},
		{"unstable ahead", "1.87.15-t1a2b", "1.86.2", &tailcfg.ClientVersion{RunningLatest: true}},
		{"two digit minor", "1.9.0", "1.10.0", &tailcfg.ClientVersion{LatestVersion: "1.10.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, clientversion.For(tt.running, tt.latest))
		})
	}
}
