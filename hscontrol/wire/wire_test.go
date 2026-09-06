package wire

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"net/netip"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/ipproto"
	"tailscale.com/types/key"
	"tailscale.com/types/opt"
)

// deterministic is the production option set plus the map-key sort v1
// always applies, so both encoders can be compared byte for byte.
var deterministic = json.JoinOptions(options, json.Deterministic(true))

// messages lists every control protocol message type the server writes or
// reads through this package, each filled with a value in every exported
// field so that the comparison covers the whole type graph.
func messages(t *testing.T) map[string]any {
	t.Helper()

	return map[string]any{
		"MapResponse":             filled(t, &tailcfg.MapResponse{}),
		"MapRequest":              filled(t, &tailcfg.MapRequest{}),
		"RegisterRequest":         filled(t, &tailcfg.RegisterRequest{}),
		"RegisterResponse":        filled(t, &tailcfg.RegisterResponse{}),
		"DERPAdmitClientRequest":  filled(t, &tailcfg.DERPAdmitClientRequest{}),
		"DERPAdmitClientResponse": filled(t, &tailcfg.DERPAdmitClientResponse{}),
		"EarlyNoise":              filled(t, &tailcfg.EarlyNoise{}),
		"SSHAction":               filled(t, &tailcfg.SSHAction{}),
		"ZeroMapResponse":         &tailcfg.MapResponse{},
		"KeepAlive":               &tailcfg.MapResponse{KeepAlive: true},
	}
}

func TestMarshalMatchesV1(t *testing.T) {
	t.Parallel()

	for name, msg := range messages(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want, err := jsonv1.Marshal(msg)
			require.NoError(t, err)

			got, err := json.Marshal(msg, deterministic)
			require.NoError(t, err)

			require.Equal(t, string(want), string(got), "v2 output must be byte-identical to v1")

			// Without the sort the output must still decode to the same value.
			unsorted, err := Marshal(msg)
			require.NoError(t, err)

			back := reflect.New(reflect.TypeOf(msg).Elem()).Interface()
			require.NoError(t, jsonv1.Unmarshal(unsorted, back))
			require.True(t, reflect.DeepEqual(msg, back), "%s: unsorted output decodes differently", name)
		})
	}
}

func TestUnmarshalMatchesV1(t *testing.T) {
	t.Parallel()

	for name, msg := range messages(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, err := jsonv1.Marshal(msg)
			require.NoError(t, err)

			wantValue := reflect.New(reflect.TypeOf(msg).Elem()).Interface()
			require.NoError(t, jsonv1.Unmarshal(data, wantValue))

			gotValue := reflect.New(reflect.TypeOf(msg).Elem()).Interface()
			require.NoError(t, Unmarshal(data, gotValue))

			require.True(t, reflect.DeepEqual(wantValue, gotValue), "%s: v2 decodes differently from v1", name)

			fromReader := reflect.New(reflect.TypeOf(msg).Elem()).Interface()
			require.NoError(t, UnmarshalRead(bytes.NewReader(data), fromReader))
			require.True(t, reflect.DeepEqual(wantValue, fromReader), "%s: UnmarshalRead differs", name)
		})
	}
}

// TestUnmarshalIsCaseInsensitive pins the v1 behaviour older or third-party
// clients may rely on.
func TestUnmarshalIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	var req tailcfg.MapRequest

	require.NoError(t, Unmarshal([]byte(`{"version":123,"COMPRESS":"zstd"}`), &req))
	require.Equal(t, tailcfg.CapabilityVersion(123), req.Version)
	require.Equal(t, "zstd", req.Compress)
}

func TestMarshalWrite(t *testing.T) {
	t.Parallel()

	msg := filled(t, &tailcfg.MapResponse{})

	var buf bytes.Buffer

	require.NoError(t, MarshalWrite(&buf, msg))

	// Map order is unspecified without the sort, so compare decoded values.
	var back tailcfg.MapResponse

	require.NoError(t, Unmarshal(buf.Bytes(), &back))
	require.True(t, reflect.DeepEqual(msg, &back), "MarshalWrite output decodes differently")
}

func BenchmarkMarshalMapResponseV1(b *testing.B) {
	msg := benchmarkMapResponse(b)

	b.ReportAllocs()

	for b.Loop() {
		_, err := jsonv1.Marshal(msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalMapResponse(b *testing.B) {
	msg := benchmarkMapResponse(b)

	b.ReportAllocs()

	for b.Loop() {
		_, err := Marshal(msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalMapRequestV1(b *testing.B) {
	data := benchmarkMapRequest(b)

	b.ReportAllocs()

	for b.Loop() {
		var req tailcfg.MapRequest

		err := jsonv1.Unmarshal(data, &req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalMapRequest(b *testing.B) {
	data := benchmarkMapRequest(b)

	b.ReportAllocs()

	for b.Loop() {
		var req tailcfg.MapRequest

		err := Unmarshal(data, &req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

const benchmarkPeers = 200

// benchmarkMapResponse is a full map for a tailnet of benchmarkPeers nodes,
// the shape sent on every connect and after every policy change.
func benchmarkMapResponse(tb testing.TB) *tailcfg.MapResponse {
	tb.Helper()

	msg := filled(tb, &tailcfg.MapResponse{})
	peer := filled(tb, &tailcfg.Node{})

	msg.Peers = make([]*tailcfg.Node, 0, benchmarkPeers)
	for i := range benchmarkPeers {
		clone := *peer
		clone.ID = tailcfg.NodeID(i)
		msg.Peers = append(msg.Peers, &clone)
	}

	return msg
}

func benchmarkMapRequest(tb testing.TB) []byte {
	tb.Helper()

	data, err := jsonv1.Marshal(filled(tb, &tailcfg.MapRequest{}))
	require.NoError(tb, err)

	return data
}

// filled returns v with every exported field of its type graph set to a
// non-zero value, so a comparison over it exercises every field.
func filled[T any](tb testing.TB, v *T) *T {
	tb.Helper()

	seed := 0
	fill(reflect.ValueOf(v).Elem(), &seed, 0)

	return v
}

// maxFillDepth stops recursion on self-referential types.
const maxFillDepth = 8

func fill(v reflect.Value, seed *int, depth int) {
	if !v.CanSet() || depth > maxFillDepth {
		return
	}

	if fillKnown(v, seed) {
		return
	}

	switch v.Kind() {
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem(), seed, depth+1)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), seed, depth+1)
			}
		}
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			fill(s.Index(i), seed, depth+1)
		}

		v.Set(s)
	case reflect.Array:
		for i := range v.Len() {
			fill(v.Index(i), seed, depth+1)
		}
	case reflect.Map:
		m := reflect.MakeMap(v.Type())

		for range 2 {
			k := reflect.New(v.Type().Key()).Elem()
			fill(k, seed, depth+1)

			e := reflect.New(v.Type().Elem()).Elem()
			fill(e, seed, depth+1)
			m.SetMapIndex(k, e)
		}

		v.Set(m)
	case reflect.String:
		*seed++
		v.SetString("s" + strconv.Itoa(*seed))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		*seed++
		v.SetInt(int64(*seed))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		*seed++
		v.SetUint(uint64(*seed))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Interface:
		v.Set(reflect.ValueOf("interface"))
	case reflect.Invalid, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		// Not representable in JSON; left zero.
	}
}

// fillKnown sets types whose JSON form needs a valid value rather than an
// arbitrary one: keys, addresses, times and the enumerations with text
// marshalers.
func fillKnown(v reflect.Value, seed *int) bool {
	*seed++

	var value any

	switch v.Type() {
	case reflect.TypeFor[time.Time]():
		value = time.Date(2026, time.September, 6, 12, 0, 0, *seed, time.UTC)
	case reflect.TypeFor[time.Duration]():
		value = time.Duration(*seed) * time.Millisecond
	case reflect.TypeFor[netip.Addr]():
		value = netip.AddrFrom4([4]byte{100, 64, 0, byte(*seed)})
	case reflect.TypeFor[netip.Prefix]():
		value = netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(*seed), 0, 0}), 16)
	case reflect.TypeFor[netip.AddrPort]():
		value = netip.AddrPortFrom(netip.AddrFrom4([4]byte{100, 64, 0, byte(*seed)}), 41641)
	case reflect.TypeFor[key.NodePublic]():
		value = key.NewNode().Public()
	case reflect.TypeFor[key.MachinePublic]():
		value = key.NewMachine().Public()
	case reflect.TypeFor[key.DiscoPublic]():
		value = key.NewDisco().Public()
	case reflect.TypeFor[key.NLPublic]():
		value = key.NewNLPrivate().Public()
	case reflect.TypeFor[key.ChallengePublic]():
		value = key.NewChallenge().Public()
	case reflect.TypeFor[opt.Bool]():
		value = opt.NewBool(true)
	case reflect.TypeFor[tailcfg.RawMessage]():
		value = tailcfg.RawMessage(`{"k":` + strconv.Itoa(*seed) + `}`)
	case reflect.TypeFor[tailcfg.MachineStatus]():
		value = tailcfg.MachineAuthorized
	case reflect.TypeFor[tailcfg.SignatureType]():
		value = tailcfg.SignatureV1
	case reflect.TypeFor[ipproto.Proto]():
		value = ipproto.TCP
	default:
		return false
	}

	v.Set(reflect.ValueOf(value))

	return true
}
