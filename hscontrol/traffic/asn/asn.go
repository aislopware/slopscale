// Package asn maps an address to the network (autonomous system) and
// country it belongs to, from iptoasn.com's ip2asn-combined table, which
// is in the public domain (PDDL) and updated hourly. The traffic monitor
// names destinations with it: "Google, US" says more than 142.250.1.1.
package asn

import (
	"bufio"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DefaultURL is where the table is published.
const DefaultURL = "https://iptoasn.com/data/ip2asn-combined.tsv.gz"

// maxDownload bounds the compressed table; it is about 9 MB.
const maxDownload = 128 << 20

// DefaultMinRanges is the fewest ranges a downloaded table must hold to
// replace the one in use: a truncated or wrong file parses into a
// handful, which would quietly strip every destination of its network.
// The real table holds about half a million.
const DefaultMinRanges = 100000

// Errors a load reports.
var (
	ErrTooSmall    = errors.New("ASN table holds too few ranges")
	ErrBadLine     = errors.New("malformed ASN table line")
	ErrHTTPStatus  = errors.New("unexpected HTTP status")
	ErrNoCacheFile = errors.New("no cached ASN table")
)

// Info is what the table knows about an address.
type Info struct {
	ASN     uint32
	Country string
	Name    string
}

type range4 struct {
	start, end uint32
	asn        uint32
	country    [2]byte
}

type range6 struct {
	start, end netip.Addr
	asn        uint32
	country    [2]byte
}

// Table is a parsed ASN table. It is immutable once built, so lookups
// need no lock.
type Table struct {
	v4    []range4
	v6    []range6
	names map[uint32]string
}

// Len is how many ranges the table holds.
func (t *Table) Len() int {
	if t == nil {
		return 0
	}

	return len(t.v4) + len(t.v6)
}

// Lookup returns what the table knows about addr; false when no range
// holds it or the range is not routed.
func (t *Table) Lookup(addr netip.Addr) (Info, bool) {
	if t == nil || !addr.IsValid() {
		return Info{}, false
	}

	addr = addr.Unmap()

	if addr.Is4() {
		v := v4Uint(addr)

		i, found := slices.BinarySearchFunc(t.v4, v, func(r range4, v uint32) int { return cmp.Compare(r.start, v) })
		if !found {
			i--
		}

		if i < 0 || v > t.v4[i].end {
			return Info{}, false
		}

		return t.info(t.v4[i].asn, t.v4[i].country), true
	}

	i, found := slices.BinarySearchFunc(t.v6, addr, func(r range6, a netip.Addr) int { return r.start.Compare(a) })
	if !found {
		i--
	}

	if i < 0 || addr.Compare(t.v6[i].end) > 0 {
		return Info{}, false
	}

	return t.info(t.v6[i].asn, t.v6[i].country), true
}

// Name is the network's name, empty when the table does not know it.
func (t *Table) Name(asn uint32) string {
	if t == nil {
		return ""
	}

	return t.names[asn]
}

func (t *Table) info(asn uint32, country [2]byte) Info {
	info := Info{ASN: asn, Name: t.names[asn]}
	if country != [2]byte{} {
		info.Country = string(country[:])
	}

	return info
}

func v4Uint(a netip.Addr) uint32 {
	b := a.As4()

	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// Parse reads the table, gzip-compressed or not: one range per line,
// "start<TAB>end<TAB>asn<TAB>country<TAB>name". Ranges with AS number 0
// are not routed and are left out.
func Parse(r io.Reader) (*Table, error) {
	br := bufio.NewReaderSize(r, readBufferSize)

	magic, peekErr := br.Peek(2)
	if peekErr == nil && bytes.Equal(magic, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("opening the gzip stream: %w", err)
		}
		defer gz.Close()

		br = bufio.NewReaderSize(gz, readBufferSize)
	}

	t := &Table{names: make(map[uint32]string)}
	scanner := bufio.NewScanner(br)
	line := 0

	for scanner.Scan() {
		line++

		text := scanner.Text()
		if text == "" {
			continue
		}

		err := t.addLine(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
	}

	err := scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("reading the ASN table: %w", err)
	}

	slices.SortFunc(t.v4, func(a, b range4) int { return cmp.Compare(a.start, b.start) })
	slices.SortFunc(t.v6, func(a, b range6) int { return a.start.Compare(b.start) })

	return t, nil
}

const tableFields = 5

// readBufferSize is the read buffer for the table and its decompressor.
const readBufferSize = 64 << 10

func (t *Table) addLine(text string) error {
	fields := strings.SplitN(text, "\t", tableFields)
	if len(fields) != tableFields {
		return fmt.Errorf("%w: %d fields", ErrBadLine, len(fields))
	}

	start, err := netip.ParseAddr(fields[0])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadLine, err)
	}

	end, err := netip.ParseAddr(fields[1])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadLine, err)
	}

	if start.Is4() != end.Is4() || end.Less(start) {
		return fmt.Errorf("%w: range %s-%s", ErrBadLine, start, end)
	}

	number, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadLine, err)
	}

	if number == 0 {
		return nil
	}

	asn := uint32(number)

	var country [2]byte
	if cc := fields[3]; len(cc) == 2 && cc != "ZZ" {
		copy(country[:], strings.ToUpper(cc))
	}

	if _, ok := t.names[asn]; !ok {
		t.names[asn] = strings.TrimSpace(fields[4])
	}

	if start.Is4() {
		t.v4 = append(t.v4, range4{start: v4Uint(start), end: v4Uint(end), asn: asn, country: country})
	} else {
		t.v6 = append(t.v6, range6{start: start, end: end, asn: asn, country: country})
	}

	return nil
}

// Source is where a table comes from: a URL, with a copy kept on disk so
// a restart has names before the first download finishes, and so a
// failed download leaves the last good table.
type Source struct {
	URL       string
	CachePath string
	Client    *http.Client
	// MinRanges overrides [DefaultMinRanges] when set.
	MinRanges int
}

// LoadCache parses the cached copy.
func (s Source) LoadCache() (*Table, error) {
	if s.CachePath == "" {
		return nil, ErrNoCacheFile
	}

	f, err := os.Open(s.CachePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoCacheFile
	}

	if err != nil {
		return nil, fmt.Errorf("opening the cached ASN table: %w", err)
	}

	defer f.Close()

	t, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parsing the cached ASN table: %w", err)
	}

	return t, nil
}

// Fetch downloads the table, keeps a copy in the cache and returns it. It
// asks only for a table newer than the cached one; when the server has
// none, it returns the cached table. A table with too few ranges is
// refused and the cache left as it was.
func (s Source) Fetch(ctx context.Context) (*Table, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building the ASN table request: %w", err)
	}

	cached, statErr := os.Stat(s.CachePath)
	if s.CachePath != "" && statErr == nil {
		req.Header.Set("If-Modified-Since", cached.ModTime().UTC().Format(http.TimeFormat))
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading the ASN table: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return s.LoadCache()
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("%w %d downloading the ASN table", ErrHTTPStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return nil, fmt.Errorf("downloading the ASN table: %w", err)
	}

	t, err := Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	if t.Len() < cmp.Or(s.MinRanges, DefaultMinRanges) {
		return nil, fmt.Errorf("%w: %d", ErrTooSmall, t.Len())
	}

	err = s.writeCache(body, resp.Header.Get("Last-Modified"))
	if err != nil {
		return nil, err
	}

	return t, nil
}

// writeCache replaces the cached copy atomically and dates it as the
// server did, so the next request asks only for something newer.
func (s Source) writeCache(body []byte, lastModified string) error {
	if s.CachePath == "" {
		return nil
	}

	err := os.MkdirAll(filepath.Dir(s.CachePath), 0o750)
	if err != nil {
		return fmt.Errorf("creating the ASN cache directory: %w", err)
	}

	tmp := s.CachePath + ".tmp"

	err = os.WriteFile(tmp, body, 0o600)
	if err != nil {
		return fmt.Errorf("writing the ASN cache: %w", err)
	}

	modified, parseErr := http.ParseTime(lastModified)
	if parseErr == nil {
		_ = os.Chtimes(tmp, time.Now(), modified)
	}

	err = os.Rename(tmp, s.CachePath)
	if err != nil {
		return fmt.Errorf("replacing the ASN cache: %w", err)
	}

	return nil
}
