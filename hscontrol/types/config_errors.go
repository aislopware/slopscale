package types

import (
	"errors"
	"fmt"
	"strings"
)

// ErrConfig matches any [ConfigError], so errors.Is(err, ErrConfig) tells
// a configuration problem apart from any other failure.
var ErrConfig = errors.New("config validation")

// ConfigError is one configuration rule the operator's settings break,
// rendered by Error as a block naming the keys, the values they hold and
// the way out.
type ConfigError struct {
	Reason        string
	Current       []KV
	ConflictsWith []KV
	Allowed       []string
	Minimum       string
	Maximum       string
	Detail        string
	Hint          string
	See           string

	// Cause keeps a sentinel reachable through errors.Is. It is not
	// rendered: the fields above are what the operator reads.
	Cause error
}

// KV is a configuration key and the value it holds. Strings render
// quoted, everything else with %v.
type KV struct {
	Key   string
	Value any
}

// Error renders the operator-facing block; TestConfigError_Render pins
// the format.
func (e *ConfigError) Error() string {
	var b strings.Builder

	b.WriteString("Fatal config error: ")
	b.WriteString(e.Reason)
	b.WriteString("\n")
	writeConfigErrLine(&b, "current", joinKVs(e.Current))
	writeConfigErrLine(&b, "conflicts with", joinKVs(e.ConflictsWith))
	writeConfigErrLine(&b, "allowed", joinQuoted(e.Allowed))
	writeConfigErrLine(&b, "minimum", e.Minimum)
	writeConfigErrLine(&b, "maximum", e.Maximum)
	writeConfigErrLine(&b, "why", e.Detail)
	writeConfigErrLine(&b, "hint", e.Hint)
	writeConfigErrLine(&b, "see", e.See)

	return b.String()
}

// Unwrap returns Cause so errors.Is walks through it.
func (e *ConfigError) Unwrap() error { return e.Cause }

// Is matches [ErrConfig]; errors.Is follows Unwrap for everything else.
func (e *ConfigError) Is(target error) bool {
	return target == ErrConfig
}

func writeConfigErrLine(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}

	b.WriteString("  ")
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

func joinKVs(kvs []KV) string {
	parts := make([]string, len(kvs))
	for i, kv := range kvs {
		parts[i] = kv.Key + ": " + formatKVValue(kv.Value)
	}

	return strings.Join(parts, ", ")
}

func joinQuoted(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = fmt.Sprintf("%q", s)
	}

	return strings.Join(parts, ", ")
}

func formatKVValue(v any) string {
	switch x := v.(type) {
	case nil:
		return `""`
	case string:
		return fmt.Sprintf("%q", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// configValidator collects every rule violation so the operator sees all
// of them in one start instead of one per attempt. The zero value is ready
// to use.
type configValidator struct {
	errs []error
}

// Add records a rule violation.
func (v *configValidator) Add(e *ConfigError) {
	v.errs = append(v.errs, e)
}

// AddErr records any error; nil is ignored.
func (v *configValidator) AddErr(err error) {
	if err == nil {
		return
	}

	v.errs = append(v.errs, err)
}

// Err is nil when no rule triggered, otherwise every violation joined.
// errors.Is and errors.As walk every branch of the join.
func (v *configValidator) Err() error {
	return errors.Join(v.errs...)
}

// ConfigErrors returns every [ConfigError] in an error tree, following
// both single and joined Unwrap chains.
func ConfigErrors(err error) []*ConfigError {
	var out []*ConfigError

	walkErrTree(err, func(e error) {
		// A plain assertion, not errors.As: errors.As would also find the
		// ConfigError below a wrapper, counting it once more when the walk
		// reaches it.
		//nolint:errorlint // see above
		if ce, ok := e.(*ConfigError); ok {
			out = append(out, ce)
		}
	})

	return out
}

func walkErrTree(err error, fn func(error)) {
	if err == nil {
		return
	}

	fn(err)

	// The switch asks what this node exposes, not the chain below it, to
	// pick the single or the joined traversal.
	//nolint:errorlint // see above
	switch x := err.(type) {
	case interface{ Unwrap() error }:
		walkErrTree(x.Unwrap(), fn)
	case interface{ Unwrap() []error }:
		for _, b := range x.Unwrap() {
			walkErrTree(b, fn)
		}
	}
}
