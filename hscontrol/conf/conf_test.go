package conf

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPrefix = "conftest"

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

// loadFile resets the store and reads body as the config file with the
// test env prefix active.
func loadFile(t *testing.T, body string) {
	t.Helper()

	Reset()
	t.Cleanup(Reset)

	SetConfigFile(writeFile(t, t.TempDir(), "config.yaml", body))
	SetEnvPrefix(testPrefix)
	require.NoError(t, ReadInConfig())
}

func TestPrecedence(t *testing.T) {
	t.Setenv("CONFTEST_LOG_LEVEL", "env")
	t.Setenv("CONFTEST_ONLY_ENV", "yes")

	loadFile(t, "log:\n  level: file\n  format: file\nfile_only: 1\n")
	SetDefault("log.level", "default")
	SetDefault("log.format", "default")
	SetDefault("default_only", "d")
	Set("log.format", "override")

	tests := []struct {
		key  string
		want string
	}{
		{"log.level", "env"},
		{"log.format", "override"},
		{"file_only", "1"},
		{"default_only", "d"},
		{"only_env", "yes"},
		{"LOG.LEVEL", "env"},
		{"missing", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, GetString(tt.key), tt.key)
	}

	for _, key := range []string{"log.level", "log.format", "file_only", "default_only", "only_env", "log"} {
		assert.True(t, IsSet(key), key)
	}

	assert.False(t, IsSet("missing"))
}

func TestEnvDisabledUntilPrefixSet(t *testing.T) {
	t.Setenv("CONFTEST_LOG_LEVEL", "env")

	Reset()
	t.Cleanup(Reset)

	SetDefault("log.level", "default")
	assert.Equal(t, "default", GetString("log.level"))
	assert.False(t, IsSet("other"))

	SetEnvPrefix(testPrefix)
	assert.Equal(t, "env", GetString("log.level"))
}

// An empty variable is an explicit empty value, not a request for the
// default: the only way to clear a list from the environment.
func TestEmptyEnvCountsAsSet(t *testing.T) {
	t.Setenv("CONFTEST_DERP_URLS", "")
	t.Setenv("CONFTEST_NAME", "")

	loadFile(t, "name: from-file\n")
	SetDefault("derp.urls", []string{"https://example.com/derpmap"})

	assert.True(t, IsSet("derp.urls"))
	assert.Empty(t, GetStringSlice("derp.urls"))
	assert.Empty(t, GetString("name"))
}

func TestTypedGetters(t *testing.T) {
	t.Setenv("CONFTEST_ENV_LIST", "a b  c")
	t.Setenv("CONFTEST_ENV_PORTS", "80 443")
	t.Setenv("CONFTEST_ENV_MAP", `{"k": "v"}`)
	t.Setenv("CONFTEST_ENV_SPLIT", `{"example.com": ["1.1.1.1", "8.8.8.8"]}`)
	t.Setenv("CONFTEST_ENV_BOOL", "false")
	t.Setenv("CONFTEST_ENV_INT", "42")
	t.Setenv("CONFTEST_ENV_DURATION", "3h")

	loadFile(t, `
str: hello
num: 7
num_str: "0o770"
flag: true
flag_str: "1"
dur: 120s
dur_bare: 500
dur_nanos: 1500
list:
  - one
  - 2
empty_list: []
ports: [443, 8443]
split:
  example.com: [1.1.1.1]
  other.org: 9.9.9.9
strmap:
  a: b
  n: 3
`)
	SetDefault("def_dur", "800ms")
	SetDefault("def_list", []string{"x", "y"})
	SetDefault("def_ports", []int{1, 2})
	SetDefault("def_map", map[string]string{})

	assert.Equal(t, "hello", GetString("str"))
	assert.Equal(t, "7", GetString("num"))
	assert.Equal(t, 7, GetInt("num"))
	assert.Equal(t, int64(7), GetInt64("num"))
	assert.Equal(t, 0o770, GetInt("num_str"))
	assert.Equal(t, 42, GetInt("env_int"))
	assert.True(t, GetBool("flag"))
	assert.True(t, GetBool("flag_str"))
	assert.False(t, GetBool("env_bool"))
	assert.False(t, GetBool("missing"))
	assert.Equal(t, 120*time.Second, GetDuration("dur"))
	assert.Equal(t, 500*time.Nanosecond, GetDuration("dur_bare"))
	assert.Equal(t, 1500*time.Nanosecond, GetDuration("dur_nanos"))
	assert.Equal(t, 800*time.Millisecond, GetDuration("def_dur"))
	assert.Equal(t, 3*time.Hour, GetDuration("env_duration"))
	assert.Equal(t, time.Duration(0), GetDuration("str"))
	assert.Equal(t, []string{"one", "2"}, GetStringSlice("list"))
	assert.Empty(t, GetStringSlice("empty_list"))
	assert.Nil(t, GetStringSlice("missing"))
	assert.Equal(t, []string{"x", "y"}, GetStringSlice("def_list"))
	assert.Equal(t, []string{"a", "b", "c"}, GetStringSlice("env_list"))
	assert.Equal(t, []int{443, 8443}, GetIntSlice("ports"))
	assert.Equal(t, []int{1, 2}, GetIntSlice("def_ports"))
	assert.Equal(t, []int{80, 443}, GetIntSlice("env_ports"))
	assert.Equal(t, map[string]string{"a": "b", "n": "3"}, GetStringMapString("strmap"))
	assert.Equal(t, map[string]string{"k": "v"}, GetStringMapString("env_map"))
	assert.Equal(t, map[string]string{}, GetStringMapString("def_map"))
	assert.Equal(t,
		map[string][]string{"example.com": {"1.1.1.1"}, "other.org": {"9.9.9.9"}},
		GetStringMapStringSlice("split"))
	assert.Equal(t,
		map[string][]string{"example.com": {"1.1.1.1", "8.8.8.8"}},
		GetStringMapStringSlice("env_split"))
	assert.Equal(t, map[string][]string{}, GetStringMapStringSlice("missing"))
}

func TestFileKeysAreCaseInsensitive(t *testing.T) {
	loadFile(t, "DNS:\n  Base_Domain: example.com\n  records:\n    - Name: a\n      Value: b\n")

	assert.Equal(t, "example.com", GetString("dns.base_domain"))

	var records []struct {
		Name  string
		Value string
	}

	require.NoError(t, UnmarshalKey("dns.records", &records))
	require.Len(t, records, 1)
	assert.Equal(t, "a", records[0].Name)
	assert.Equal(t, "b", records[0].Value)
}

func TestSetReplacesFileAndDefault(t *testing.T) {
	loadFile(t, "list: [a]\n")
	SetDefault("list", []string{"d"})
	SetDefault("flag", false)

	Set("list", []string{"s"})
	Set("flag", true)
	Set("cli.api_key", "")

	assert.Equal(t, []string{"s"}, GetStringSlice("list"))
	assert.True(t, GetBool("flag"))
	assert.True(t, IsSet("cli.api_key"))
	assert.Empty(t, GetString("cli.api_key"))
}

func TestReset(t *testing.T) {
	t.Setenv("CONFTEST_KEY", "env")

	loadFile(t, "key: file\n")
	SetDefault("other", 1)
	Set("third", 3)
	Reset()

	assert.False(t, IsSet("key"))
	assert.False(t, IsSet("other"))
	assert.False(t, IsSet("third"))
	assert.Empty(t, ConfigFileUsed())
	require.ErrorIs(t, ReadInConfig(), ErrConfigFileNotFound)
}

func TestSearchPaths(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	empty := t.TempDir()
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yml", "found: true\n")

	SetConfigName("config")
	AddConfigPath(empty)
	AddConfigPath(dir)

	require.NoError(t, ReadInConfig())
	assert.Equal(t, path, ConfigFileUsed())
	assert.True(t, GetBool("found"))
}

// SetConfigName after SetConfigFile returns to searching, the way a test
// that loads a directory after another loaded a file expects.
func TestSetConfigNameForgetsExplicitFile(t *testing.T) {
	loadFile(t, "a: 1\n")

	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", "a: 2\n")
	SetConfigName("config")
	AddConfigPath(dir)

	require.NoError(t, ReadInConfig())
	assert.Equal(t, 2, GetInt("a"))
}

func TestSearchPathsExpandEnv(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	dir := t.TempDir()
	t.Setenv("CONFTEST_HOME", dir)
	writeFile(t, dir, "config.yaml", "found: true\n")

	SetConfigName("config")
	AddConfigPath("$CONFTEST_HOME")

	require.NoError(t, ReadInConfig())
	assert.True(t, GetBool("found"))
}

func TestReadErrors(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	SetConfigName("config")
	AddConfigPath(t.TempDir())
	require.ErrorIs(t, ReadInConfig(), ErrConfigFileNotFound)

	SetConfigFile(filepath.Join(t.TempDir(), "missing.yaml"))

	err := ReadInConfig()
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrConfigFileNotFound)
	require.ErrorIs(t, err, os.ErrNotExist)

	SetConfigFile(writeFile(t, t.TempDir(), "config.toml", "a = 1\n"))
	require.ErrorIs(t, ReadInConfig(), ErrUnsupportedConfigType)

	SetConfigFile(writeFile(t, t.TempDir(), "config.yaml", "a: [\n"))
	require.Error(t, ReadInConfig())
}

func TestReadInConfigReplacesEarlierFile(t *testing.T) {
	loadFile(t, "a: 1\nb: 1\n")
	SetConfigFile(writeFile(t, t.TempDir(), "config.yaml", "a: 2\n"))
	require.NoError(t, ReadInConfig())

	assert.Equal(t, 2, GetInt("a"))
	assert.False(t, IsSet("b"))
}

func TestAllSettingsAndWriteConfigAs(t *testing.T) {
	t.Setenv("CONFTEST_LOG_LEVEL", "env")

	loadFile(t, "log:\n  level: file\nlist: [a, b]\n")
	SetDefault("log.format", "text")
	SetDefault("only_default", true)
	Set("log.format", "json")

	want := map[string]any{
		"log":          map[string]any{"level": "env", "format": "json"},
		"list":         []any{"a", "b"},
		"only_default": true,
	}
	assert.Equal(t, want, AllSettings())

	path := filepath.Join(t.TempDir(), "dump.yaml")
	require.NoError(t, WriteConfigAs(path))

	Reset()
	SetConfigFile(path)
	require.NoError(t, ReadInConfig())
	assert.Equal(t, "env", GetString("log.level"))
	assert.Equal(t, "json", GetString("log.format"))
	assert.Equal(t, []string{"a", "b"}, GetStringSlice("list"))
	assert.True(t, GetBool("only_default"))
}

func TestUnmarshalKeyDurationAndTags(t *testing.T) {
	loadFile(t, "job:\n  every: 5m\n  display_name: x\n")

	var job struct {
		Every time.Duration
		Name  string `mapstructure:"display_name"`
	}

	require.NoError(t, UnmarshalKey("job", &job))
	assert.Equal(t, 5*time.Minute, job.Every)
	assert.Equal(t, "x", job.Name)
	require.Error(t, UnmarshalKey("job.every", &job))
}
