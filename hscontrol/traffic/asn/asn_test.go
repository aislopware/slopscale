package asn_test

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic/asn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sample is a slice of the real ip2asn-combined table, in its format:
// tab separated, v4 and v6 in one file, unrouted ranges with AS 0.
const sample = "1.0.0.0\t1.0.0.255\t13335\tUS\tCLOUDFLARENET\n" +
	"1.0.1.0\t1.0.3.255\t0\tNone\tNot routed\n" +
	"8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n" +
	"113.160.0.0\t113.191.255.255\t45899\tVN\tVNPT-AS-VN VNPT Corp\n" +
	"142.250.0.0\t142.250.255.255\t15169\tUS\tGOOGLE\n" +
	"2001:4860::\t2001:4860:ffff:ffff:ffff:ffff:ffff:ffff\t15169\tUS\tGOOGLE\n" +
	"2606:4700::\t2606:4700:ffff:ffff:ffff:ffff:ffff:ffff\t13335\tUS\tCLOUDFLARENET\n"

func gzipped(t *testing.T, s string) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := gzip.NewWriter(&buf)
	_, err := w.Write([]byte(s))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return buf.Bytes()
}

func TestParseAndLookup(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][]byte{"plain": []byte(sample), "gzip": gzipped(t, sample)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			table, err := asn.Parse(bytes.NewReader(input))
			require.NoError(t, err)
			assert.Equal(t, 6, table.Len(), "the unrouted range is left out")

			cases := []struct {
				addr string
				want asn.Info
				ok   bool
			}{
				{"1.0.0.1", asn.Info{ASN: 13335, Country: "US", Name: "CLOUDFLARENET"}, true},
				{"1.0.0.255", asn.Info{ASN: 13335, Country: "US", Name: "CLOUDFLARENET"}, true},
				{"1.0.2.1", asn.Info{}, false},
				{"8.8.8.8", asn.Info{ASN: 15169, Country: "US", Name: "GOOGLE"}, true},
				{"113.161.1.1", asn.Info{ASN: 45899, Country: "VN", Name: "VNPT-AS-VN VNPT Corp"}, true},
				{"::ffff:142.250.1.1", asn.Info{ASN: 15169, Country: "US", Name: "GOOGLE"}, true},
				{"0.0.0.1", asn.Info{}, false},
				{"255.255.255.255", asn.Info{}, false},
				{"2001:4860:4860::8888", asn.Info{ASN: 15169, Country: "US", Name: "GOOGLE"}, true},
				{"2606:4700::1111", asn.Info{ASN: 13335, Country: "US", Name: "CLOUDFLARENET"}, true},
				{"2a00::1", asn.Info{}, false},
				{"::1", asn.Info{}, false},
			}

			for _, c := range cases {
				got, ok := table.Lookup(netip.MustParseAddr(c.addr))
				assert.Equal(t, c.ok, ok, c.addr)
				assert.Equal(t, c.want, got, c.addr)
			}

			assert.Equal(t, "GOOGLE", table.Name(15169))
			assert.Empty(t, table.Name(1))
		})
	}
}

func TestNilTable(t *testing.T) {
	t.Parallel()

	var table *asn.Table

	_, ok := table.Lookup(netip.MustParseAddr("8.8.8.8"))
	assert.False(t, ok)
	assert.Zero(t, table.Len())
	assert.Empty(t, table.Name(15169))
}

func TestParseRejectsMalformed(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"too few fields":  "1.0.0.0\t1.0.0.255\t13335\n",
		"bad start":       "1.0.0\t1.0.0.255\t13335\tUS\tX\n",
		"bad end":         "1.0.0.0\tnope\t13335\tUS\tX\n",
		"bad asn":         "1.0.0.0\t1.0.0.255\tAS13335\tUS\tX\n",
		"mixed families":  "1.0.0.0\t2001::1\t13335\tUS\tX\n",
		"reversed range":  "1.0.0.9\t1.0.0.1\t13335\tUS\tX\n",
		"asn overflowing": "1.0.0.0\t1.0.0.255\t99999999999\tUS\tX\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := asn.Parse(strings.NewReader(input))
			require.ErrorIs(t, err, asn.ErrBadLine)
		})
	}

	_, err := asn.Parse(bytes.NewReader([]byte{0x1f, 0x8b, 0x00}))
	require.Error(t, err, "a broken gzip stream")
}

func TestFetchKeepsACache(t *testing.T) {
	t.Parallel()

	body := gzipped(t, sample)
	modified := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)

	var (
		status   atomic.Int32
		requests atomic.Int32
		lastIMS  atomic.Value
	)

	status.Store(http.StatusOK)
	lastIMS.Store("")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		lastIMS.Store(r.Header.Get("If-Modified-Since"))

		ims, err := http.ParseTime(r.Header.Get("If-Modified-Since"))
		if err == nil && !modified.After(ims) {
			w.WriteHeader(http.StatusNotModified)

			return
		}

		code := int(status.Load())
		if code != http.StatusOK {
			w.WriteHeader(code)

			return
		}

		w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	cache := filepath.Join(t.TempDir(), "asn", "ip2asn.tsv.gz")
	src := asn.Source{URL: srv.URL, CachePath: cache, Client: srv.Client(), MinRanges: 1}

	_, err := src.LoadCache()
	require.ErrorIs(t, err, asn.ErrNoCacheFile, "nothing is cached before the first download")

	table, err := src.Fetch(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 6, table.Len())
	assert.Empty(t, lastIMS.Load(), "the first download is unconditional")

	info, err := os.Stat(cache)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(modified), "the cache is dated as the server dated the table")

	assert.True(t, src.CachedAt().Equal(modified))

	_, err = src.Fetch(t.Context())
	require.ErrorIs(t, err, asn.ErrNotModified, "the table in use stays without parsing the cache again")
	assert.NotEmpty(t, lastIMS.Load())

	cached, err := src.LoadCache()
	require.NoError(t, err)
	assert.Equal(t, 6, cached.Len())

	// A server error and a table too small to be real both fail and
	// leave the cache alone.
	require.NoError(t, os.Chtimes(cache, time.Now(), modified.Add(-time.Hour)))
	status.Store(http.StatusBadGateway)

	_, err = src.Fetch(t.Context())
	require.ErrorIs(t, err, asn.ErrHTTPStatus)

	status.Store(http.StatusOK)

	strict := src
	strict.MinRanges = 1000

	_, err = strict.Fetch(t.Context())
	require.ErrorIs(t, err, asn.ErrTooSmall)

	cached, err = src.LoadCache()
	require.NoError(t, err)
	assert.Equal(t, 6, cached.Len(), "the cache survives failed downloads")
	assert.Equal(t, int32(4), requests.Load())
}
