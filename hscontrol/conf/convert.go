package conf

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"
)

// The converters below accept what the three sources produce: Go values
// from [Set] and [SetDefault], YAML scalars, lists and maps from the file,
// and strings from the environment. Anything unparsable yields the zero
// value, the way a missing key does.

func toString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case []byte:
		return string(val)
	case fmt.Stringer:
		return val.String()
	case error:
		return val.Error()
	default:
		// Lists and maps have no string form; a key read with the wrong
		// getter is empty, like a missing one.
		return ""
	}
}

func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(val))

		return b
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	default:
		return false
	}
}

func toInt(v any) int {
	return int(toInt64(v))
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int64:
		return val
	case int32:
		return int64(val)
	case uint:
		return int64(val) //nolint:gosec // G115: a config value, not attacker input
	case uint64:
		return int64(val) //nolint:gosec // G115: a config value, not attacker input
	case float64:
		return int64(val)
	case bool:
		if val {
			return 1
		}

		return 0
	case string:
		// Base 0 takes 0x, 0o and 0b prefixes, like a Go literal.
		i, _ := strconv.ParseInt(trimZeroDecimal(strings.TrimSpace(val)), 0, 64)

		return i
	default:
		return 0
	}
}

// trimZeroDecimal drops a ".0" tail so "10.0" reads as 10.
func trimZeroDecimal(s string) string {
	before, after, found := strings.Cut(s, ".")
	if !found || strings.Trim(after, "0") != "" {
		return s
	}

	return before
}

func toDuration(v any) time.Duration {
	switch val := v.(type) {
	case time.Duration:
		return val
	case int:
		return time.Duration(val)
	case int64:
		return time.Duration(val)
	case float64:
		return time.Duration(val)
	case string:
		s := strings.TrimSpace(val)
		if !strings.ContainsAny(s, "nsuµmh") {
			s += "ns"
		}

		d, _ := time.ParseDuration(s)

		return d
	default:
		return 0
	}
}

func toStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		out := make([]string, len(val))
		copy(out, val)

		return out
	case []any:
		var out []string
		for _, item := range val {
			out = append(out, toString(item))
		}

		return out
	case []int:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, strconv.Itoa(item))
		}

		return out
	case string:
		return strings.Fields(val)
	case nil:
		return nil
	default:
		return []string{toString(v)}
	}
}

func toIntSlice(v any) []int {
	switch val := v.(type) {
	case []int:
		out := make([]int, len(val))
		copy(out, val)

		return out
	case []any:
		out := make([]int, 0, len(val))
		for _, item := range val {
			out = append(out, toInt(item))
		}

		return out
	case string:
		fields := strings.Fields(val)

		out := make([]int, 0, len(fields))
		for _, field := range fields {
			out = append(out, toInt(field))
		}

		return out
	default:
		return []int{}
	}
}

func toStringMapString(v any) map[string]string {
	out := map[string]string{}

	switch val := v.(type) {
	case map[string]string:
		maps.Copy(out, val)
	case map[string]any:
		for k, item := range val {
			out[k] = toString(item)
		}
	case string:
		_ = json.Unmarshal([]byte(val), &out)
	}

	return out
}

func toStringMapStringSlice(v any) map[string][]string {
	out := map[string][]string{}

	switch val := v.(type) {
	case map[string][]string:
		for k, item := range val {
			out[k] = toStringSlice(item)
		}
	case map[string]string:
		for k, item := range val {
			out[k] = strings.Fields(item)
		}
	case map[string]any:
		for k, item := range val {
			out[k] = stringsOf(item)
		}
	case string:
		_ = json.Unmarshal([]byte(val), &out)
	}

	return out
}

// stringsOf turns one map value into a list: lists element-wise, anything
// else as a single entry, except a string, which is split on whitespace.
func stringsOf(v any) []string {
	switch val := v.(type) {
	case []any, []string:
		return toStringSlice(val)
	case string:
		return strings.Fields(val)
	default:
		return []string{toString(val)}
	}
}
