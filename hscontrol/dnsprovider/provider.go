// Package dnsprovider publishes ACME DNS-01 challenge records in the zone
// that holds the tailnet's MagicDNS names, so that machines can answer
// challenges for their own names, and the server for its own hostname;
// see docs/ref/https-certificates.md.
//
// A provider is a libdns provider. Cloudflare, RFC 2136 and the command
// provider are written here; acme-dns, Hetzner and PowerDNS come from
// libdns. Every one of them stays on the libdns major that certmagic
// uses, since certmagic publishes the server's own challenges through
// the same provider.
package dnsprovider

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/libdns/hetzner/v2"
	"github.com/libdns/libdns"
	"github.com/libdns/powerdns"
)

// Provider writes ACME DNS-01 challenge records into a zone.
//
// It is append and delete only: a wildcard order puts two values on the
// same name, so SetRecords would replace the value the CA is validating.
type Provider interface {
	libdns.RecordAppender
	libdns.RecordDeleter
}

// Isolator is implemented by a provider that can answer several names'
// challenges from one record, as acme-dns does for names sharing a
// registration. The CA accepts a value found there for any of those
// names, so a node only gets a certificate for a name the provider
// isolates.
type Isolator interface {
	// Isolates reports whether name's challenge is answered apart from
	// every other name's, once CheckIsolation passed.
	Isolates(name string) bool
	// CheckIsolation reports why node certificate names would share a
	// record.
	CheckIsolation() error
}

// providers is the compiled-in provider registry. Providers are decoded
// from configuration through their json tags, so adding one is one line
// plus its module; the set is curated because each provider is a
// dependency to keep current and its quirks become ours.
var providers = map[string]func() Provider{
	"acmedns":    func() Provider { return newAcmeDNS() },
	"cloudflare": func() Provider { return new(Cloudflare) },
	"command":    func() Provider { return new(Command) },
	"hetzner":    func() Provider { return new(hetzner.Provider) },
	"powerdns":   func() Provider { return new(powerdns.Provider) },
	"rfc2136":    func() Provider { return new(RFC2136) },
}

var (
	// ErrUnknownProvider is returned for a provider name the server does
	// not have.
	ErrUnknownProvider = errors.New("unknown DNS provider")
	// ErrProviderConfig is returned for settings the provider cannot use.
	ErrProviderConfig = errors.New("invalid DNS provider configuration")
	// ErrNameOutsideZone is returned for a record that does not belong to
	// the zone it is written to.
	ErrNameOutsideZone = errors.New("record name is outside the DNS zone")
	// ErrNotTXT is returned for a record other than TXT; challenges are
	// the only records written.
	ErrNotTXT = errors.New("only TXT records are written")
)

// pathSuffix marks a setting whose value is read from a file, keeping
// secrets out of the configuration file.
const pathSuffix = "_path"

// ProviderNames returns the names of the compiled-in providers.
func ProviderNames() []string {
	return slices.Sorted(maps.Keys(providers))
}

// New builds the named provider from its settings.
//
// settings holds the provider's keys as written in the configuration
// file. get looks up a single key and is consulted for every field of
// the provider, because environment variables only reach individual
// keys, never a map. Any string field can instead be read from a file
// named by "<field>_path", unless the provider has a field of that name
// itself. Unknown keys are an error so a typo does not silently leave a
// credential empty.
//
//nolint:ireturn // the caller wants any provider
func New(
	name string,
	settings map[string]any,
	get func(key string) (any, bool),
) (Provider, error) {
	newProvider, ok := providers[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}

	provider := newProvider()

	input := make(map[string]any, len(settings))
	for k, v := range settings {
		input[strings.ToLower(k)] = v
	}

	fields := jsonFields(provider)
	for tag, kind := range fields {
		if v, ok := get(tag); ok {
			input[tag] = v
		}

		err := readPathSetting(input, fields, tag, kind, get)
		if err != nil {
			return nil, err
		}
	}

	if p, ok := provider.(interface{ prepare(input map[string]any) }); ok {
		p.prepare(input)
	}

	// Every provider needs a credential, a server or a program: a block
	// with nothing in it is a secret that was not set, and would fail at
	// the first certificate instead of at startup.
	if len(input) == 0 {
		return nil, fmt.Errorf("%w for %s: no settings", ErrProviderConfig, name)
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName:          "json",
		WeaklyTypedInput: true,
		ErrorUnused:      true,
		Squash:           true,
		Result:           provider,
	})
	if err != nil {
		return nil, fmt.Errorf("creating decoder: %w", err)
	}

	err = decoder.Decode(input)
	if err != nil {
		return nil, fmt.Errorf("%w for %s: %w", ErrProviderConfig, name, err)
	}

	if v, ok := provider.(interface{ validate() error }); ok {
		err = v.validate()
		if err != nil {
			return nil, fmt.Errorf("%w for %s: %w", ErrProviderConfig, name, err)
		}
	}

	return provider, nil
}

// readPathSetting replaces "<tag>_path" in input with the file's
// contents under tag: the text for a string field, a JSON object for a
// map field. A map field given as a string, as an environment variable
// gives it, is a JSON object too.
func readPathSetting(
	input map[string]any,
	fields map[string]reflect.Kind,
	tag string,
	kind reflect.Kind,
	get func(key string) (any, bool),
) error {
	if s, ok := input[tag].(string); ok && kind == reflect.Map {
		var m map[string]any

		err := json.Unmarshal([]byte(s), &m)
		if err != nil {
			return fmt.Errorf("%w: %s is not a JSON object: %w", ErrProviderConfig, tag, err)
		}

		input[tag] = m
	}

	pathKey := tag + pathSuffix
	if _, isField := fields[pathKey]; isField || (kind != reflect.String && kind != reflect.Map) {
		return nil
	}

	if v, ok := get(pathKey); ok {
		input[pathKey] = v
	}

	path, ok := input[pathKey]
	if !ok {
		return nil
	}

	if _, both := input[tag]; both {
		return fmt.Errorf("%w: %s and %s are mutually exclusive", ErrProviderConfig, tag, pathKey)
	}

	value, err := os.ReadFile(os.ExpandEnv(fmt.Sprint(path)))
	if err != nil {
		return fmt.Errorf("%w: reading %s: %w", ErrProviderConfig, pathKey, err)
	}

	delete(input, pathKey)

	if kind == reflect.String {
		input[tag] = strings.TrimSpace(string(value))

		return nil
	}

	var m map[string]any

	err = json.Unmarshal(value, &m)
	if err != nil {
		return fmt.Errorf("%w: %s is not a JSON object: %w", ErrProviderConfig, pathKey, err)
	}

	input[tag] = m

	return nil
}

// jsonFields returns the json key and kind of each configurable field of
// the provider struct.
func jsonFields(provider Provider) map[string]reflect.Kind {
	t := reflect.TypeOf(provider).Elem()
	fields := make(map[string]reflect.Kind, t.NumField())

	for _, f := range reflect.VisibleFields(t) {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "" || name == "-" {
			continue
		}

		fields[name] = f.Type.Kind()
	}

	return fields
}

// txtRecords returns the fully qualified name, lowercase and without a
// trailing dot, and the value of each record, refusing any but TXT.
func txtRecords(zone string, recs []libdns.Record) ([][2]string, error) {
	out := make([][2]string, 0, len(recs))

	for _, rec := range recs {
		rr := rec.RR()
		if rr.Type != "TXT" {
			return nil, fmt.Errorf("%w: %s", ErrNotTXT, rr.Type)
		}

		name := strings.ToLower(strings.TrimSuffix(libdns.AbsoluteName(rr.Name, zone), "."))
		out = append(out, [2]string{name, rr.Data})
	}

	return out, nil
}

// inZone reports whether name is the zone or a name under it.
func inZone(name, zone string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	zone = strings.TrimSuffix(strings.ToLower(zone), ".")

	return name == zone || strings.HasSuffix(name, "."+zone)
}
