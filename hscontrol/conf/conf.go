// Package conf is the raw configuration store the server and the CLI read
// from: defaults, the YAML config file, SLOPSCALE_ environment variables and
// explicit overrides, in that order of increasing precedence. Keys are
// dotted paths ("dns.base_domain") and case-insensitive.
//
// The store is process-wide, like the configuration itself: [Reset] wipes it,
// [SetDefault] and [ReadInConfig] fill it, the Get* functions read it. koanf
// holds the layers; the environment is consulted per key at read time, so a
// variable overrides any key whether or not something else declared it.
package conf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	yamlv3 "go.yaml.in/yaml/v3"
)

const (
	delim        = "."
	envDelim     = "_"
	dumpFileMode = 0o644
	unmarshalTag = "mapstructure"
)

// ErrConfigFileNotFound is returned by [ReadInConfig] when no config file
// exists in the search paths. An explicitly named file that is missing is an
// ordinary read error instead.
var ErrConfigFileNotFound = errors.New("config file not found")

// ErrUnsupportedConfigType is returned for a config file whose extension is
// not yaml, yml or json.
var ErrUnsupportedConfigType = errors.New("unsupported config file type")

// configExtensions are the file types searched for, in order.
var configExtensions = []string{"yaml", "yml", "json"}

// store is the process-wide configuration.
type store struct {
	mu sync.RWMutex

	defaults  *koanf.Koanf
	file      *koanf.Koanf
	overrides *koanf.Koanf

	// merged is defaults, file and overrides layered, rebuilt on every
	// write so reads never mutate the store.
	merged *koanf.Koanf

	// envPrefix is empty until [SetEnvPrefix]; the environment is only
	// consulted while it is set.
	envPrefix string

	configFile  string
	configName  string
	configPaths []string
	fileUsed    string
}

var global = newStore()

func newStore() *store {
	return &store{
		defaults:  koanf.New(delim),
		file:      koanf.New(delim),
		overrides: koanf.New(delim),
		merged:    koanf.New(delim),
	}
}

// Reset empties every layer and forgets the file paths and the environment
// prefix, like a fresh process.
func Reset() {
	global.mu.Lock()
	defer global.mu.Unlock()

	global.defaults = koanf.New(delim)
	global.file = koanf.New(delim)
	global.overrides = koanf.New(delim)
	global.merged = koanf.New(delim)
	global.envPrefix = ""
	global.configFile = ""
	global.configName = ""
	global.configPaths = nil
	global.fileUsed = ""
}

// SetDefault sets the value a key has when neither the file, the environment
// nor [Set] provides one.
func SetDefault(key string, value any) {
	global.mu.Lock()
	defer global.mu.Unlock()

	_ = global.defaults.Set(normalizeKey(key), value)
	global.rebuild()
}

// Set overrides a key above every other source.
func Set(key string, value any) {
	global.mu.Lock()
	defer global.mu.Unlock()

	_ = global.overrides.Set(normalizeKey(key), value)
	global.rebuild()
}

// SetEnvPrefix turns environment lookups on: key "a.b" reads PREFIX_A_B.
func SetEnvPrefix(prefix string) {
	global.mu.Lock()
	defer global.mu.Unlock()

	global.envPrefix = strings.ToUpper(prefix)
}

// SetConfigFile names the file [ReadInConfig] reads, bypassing the search.
// An empty path is ignored.
func SetConfigFile(path string) {
	if path == "" {
		return
	}

	global.mu.Lock()
	defer global.mu.Unlock()

	global.configFile = path
}

// SetConfigName sets the base name, without extension, searched for in the
// paths added with [AddConfigPath], and forgets an explicit file so the
// search is used again.
func SetConfigName(name string) {
	if name == "" {
		return
	}

	global.mu.Lock()
	defer global.mu.Unlock()

	global.configName = name
	global.configFile = ""
}

// AddConfigPath appends a directory to search; $VARs in it are expanded.
func AddConfigPath(dir string) {
	global.mu.Lock()
	defer global.mu.Unlock()

	global.configPaths = append(global.configPaths, os.ExpandEnv(dir))
}

// ConfigFileUsed returns the path of the file the last [ReadInConfig] read,
// or an empty string.
func ConfigFileUsed() string {
	global.mu.RLock()
	defer global.mu.RUnlock()

	return global.fileUsed
}

// ReadInConfig loads the config file into the file layer, replacing what an
// earlier call loaded. Without an explicit file it returns
// [ErrConfigFileNotFound] when the search finds nothing.
func ReadInConfig() error {
	global.mu.Lock()
	defer global.mu.Unlock()

	path, err := global.findConfigFile()
	if err != nil {
		return err
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if !slices.Contains(configExtensions, ext) {
		return fmt.Errorf("%w: %q", ErrUnsupportedConfigType, path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	parsed, err := yaml.Parser().Unmarshal(raw)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	layer := koanf.New(delim)

	err = layer.Load(confmap.Provider(lowercaseKeys(parsed), ""), nil)
	if err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}

	global.file = layer
	global.fileUsed = path
	global.rebuild()

	return nil
}

func (s *store) findConfigFile() (string, error) {
	if s.configFile != "" {
		return s.configFile, nil
	}

	for _, dir := range s.configPaths {
		for _, ext := range configExtensions {
			candidate := filepath.Join(dir, s.configName+"."+ext)

			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	return "", ErrConfigFileNotFound
}

// Get returns the raw value of key from the highest source that has it, or
// nil. An environment variable is returned as its string.
func Get(key string) any {
	key = normalizeKey(key)

	global.mu.RLock()
	defer global.mu.RUnlock()

	if v, ok := global.env(key); ok {
		return v
	}

	return global.view().Get(key)
}

// IsSet reports whether any source, defaults included, has the key.
func IsSet(key string) bool {
	return Get(key) != nil
}

// GetString returns the key as a string, "" when unset.
func GetString(key string) string {
	return toString(Get(key))
}

// GetBool returns the key as a bool, false when unset or unparsable.
func GetBool(key string) bool {
	return toBool(Get(key))
}

// GetInt returns the key as an int, 0 when unset or unparsable.
func GetInt(key string) int {
	return toInt(Get(key))
}

// GetInt64 returns the key as an int64, 0 when unset or unparsable.
func GetInt64(key string) int64 {
	return toInt64(Get(key))
}

// GetDuration returns the key as a duration, 0 when unset or unparsable. A
// bare number is nanoseconds, like time.Duration itself.
func GetDuration(key string) time.Duration {
	return toDuration(Get(key))
}

// GetStringSlice returns the key as strings; an environment value is split
// on whitespace.
func GetStringSlice(key string) []string {
	return toStringSlice(Get(key))
}

// GetIntSlice returns the key as ints; an environment value is split on
// whitespace.
func GetIntSlice(key string) []int {
	return toIntSlice(Get(key))
}

// GetStringMapString returns the key as a string map; an environment value
// is parsed as a JSON object.
func GetStringMapString(key string) map[string]string {
	return toStringMapString(Get(key))
}

// GetStringMapStringSlice returns the key as a map of string lists; a string
// value is split on whitespace and an environment value is parsed as a JSON
// object.
func GetStringMapStringSlice(key string) map[string][]string {
	return toStringMapStringSlice(Get(key))
}

// UnmarshalKey decodes the key's value into out with mapstructure semantics
// (case-insensitive field names, "mapstructure" tags, weak typing).
func UnmarshalKey(key string, out any) error {
	key = normalizeKey(key)

	global.mu.RLock()
	defer global.mu.RUnlock()

	err := global.view().UnmarshalWithConf(key, out, koanf.UnmarshalConf{Tag: unmarshalTag})
	if err != nil {
		return fmt.Errorf("decoding %s: %w", key, err)
	}

	return nil
}

// AllSettings returns every known key as a nested map, environment
// overrides applied.
func AllSettings() map[string]any {
	global.mu.RLock()
	defer global.mu.RUnlock()

	view := global.view().Copy()

	for _, key := range view.Keys() {
		if v, ok := global.env(key); ok {
			_ = view.Set(key, v)
		}
	}

	return view.Raw()
}

// WriteConfigAs writes [AllSettings] to path as YAML.
func WriteConfigAs(path string) error {
	out, err := yamlv3.Marshal(AllSettings())
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	err = os.WriteFile(path, out, dumpFileMode)
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// rebuild layers the sources into merged; the caller holds the write lock.
func (s *store) rebuild() {
	merged := s.defaults.Copy()
	_ = merged.Merge(s.file)
	_ = merged.Merge(s.overrides)

	s.merged = merged
}

// view returns the merged layers; the caller holds at least the read lock.
func (s *store) view() *koanf.Koanf {
	return s.merged
}

// env looks the key up in the environment. A variable that is set to an
// empty string counts: it is the operator saying "nothing here", not a
// request for the default.
func (s *store) env(key string) (string, bool) {
	if s.envPrefix == "" {
		return "", false
	}

	name := s.envPrefix + envDelim + strings.ToUpper(strings.ReplaceAll(key, delim, envDelim))

	return os.LookupEnv(name)
}

func normalizeKey(key string) string {
	return strings.ToLower(key)
}

// lowercaseKeys lowercases every map key in a parsed document, including
// inside lists, so file keys match the case-insensitive lookups.
func lowercaseKeys(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = lowercaseValue(v)
	}

	return out
}

func lowercaseValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return lowercaseKeys(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = lowercaseValue(item)
		}

		return out
	default:
		return v
	}
}
