package sni

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/net/quic"
)

func readVector(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)

	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	require.NoError(t, err)

	return b
}

// TestQUICInitialRFCVectors decrypts the client Initial packets published
// in RFC 9001 Appendix A.2 (QUIC v1) and RFC 9369 Appendix A.2 (QUIC v2),
// which carry a ClientHello for example.com.
func TestQUICInitialRFCVectors(t *testing.T) {
	for _, name := range []string{"rfc9001-client-initial.hex", "rfc9369-client-initial.hex"} {
		t.Run(name, func(t *testing.T) {
			initial, err := ParseInitials(readVector(t, name))
			require.NoError(t, err)

			assert.Equal(t, "8394c8f03e515708", hex.EncodeToString(initial.DCID))
			require.Len(t, initial.Frames, 1)
			assert.Zero(t, initial.Frames[0].Offset)
			assert.Len(t, initial.Frames[0].Data, 241)

			var stream CryptoStream

			hello, done, err := stream.Add(initial.Frames)
			require.NoError(t, err)
			require.True(t, done)
			assert.Equal(t, Hello{ServerName: "example.com"}, hello)
		})
	}
}

func TestQUICInitialRejects(t *testing.T) {
	v1 := readVector(t, "rfc9001-client-initial.hex")

	flipped := append([]byte(nil), v1...)
	flipped[len(flipped)-40] ^= 0xff

	unknownVersion := append([]byte(nil), v1...)
	binary.BigEndian.PutUint32(unknownVersion[1:], 0xff00001d)

	longToken := append([]byte(nil), v1[:22]...)
	longToken[18] = 0x7f // a token length running past the packet

	for _, tc := range []struct {
		name string
		in   []byte
		want error
	}{
		{name: "empty", in: nil, want: ErrNotInitial},
		{name: "short header", in: []byte{0x40, 1, 2, 3, 4, 5, 6, 7}, want: ErrNotInitial},
		{name: "unknown version", in: unknownVersion, want: ErrNotInitial},
		{name: "truncated", in: v1[:600], want: ErrQUICMalformed},
		{name: "tampered ciphertext", in: flipped, want: ErrQUICMalformed},
		{name: "header only", in: v1[:22], want: ErrQUICMalformed},
		{name: "token past the end", in: longToken, want: ErrQUICMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseInitials(tc.in)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// TestCryptoStreamOutOfOrder feeds the ClientHello as small CRYPTO frames
// in reverse order over several calls, the way Chrome shuffles them over
// several Initial packets; the hello is complete only once the gap at the
// start is filled.
func TestCryptoStreamOutOfOrder(t *testing.T) {
	initial, err := ParseInitials(readVector(t, "rfc9001-client-initial.hex"))
	require.NoError(t, err)

	data := initial.Frames[0].Data

	var frames []CryptoFrame

	for off := 0; off < len(data); off += 50 {
		end := min(off+50, len(data))
		frames = append(frames, CryptoFrame{Offset: uint64(off), Data: data[off:end]})
	}

	var stream CryptoStream

	for i := len(frames) - 1; i > 0; i-- {
		_, early, addErr := stream.Add(frames[i : i+1])
		require.NoError(t, addErr)
		require.False(t, early, "complete before the first frame arrived")
	}

	// A duplicate of an already held range changes nothing.
	_, done, err := stream.Add(frames[2:3])
	require.NoError(t, err)
	require.False(t, done)

	hello, done, err := stream.Add(frames[:1])
	require.NoError(t, err)
	require.True(t, done)
	assert.Equal(t, "example.com", hello.ServerName)
}

func TestCryptoStreamRejectsFarOffsets(t *testing.T) {
	var stream CryptoStream

	_, _, err := stream.Add([]CryptoFrame{{Offset: MaxHelloSize, Data: []byte{1}}})
	require.ErrorIs(t, err, ErrTooLarge)

	_, _, err = stream.Add([]CryptoFrame{{Offset: ^uint64(0) - 1, Data: []byte{1, 2, 3}}})
	require.ErrorIs(t, err, ErrTooLarge)
}

// captureClientHello returns the bytes crypto/tls's client writes first,
// which is its ClientHello in one or more TLS records.
func captureClientHello(t *testing.T, cfg *tls.Config) []byte {
	t.Helper()

	client, server := net.Pipe()

	t.Cleanup(func() {
		client.Close()
		server.Close()
	})

	go func() {
		_ = tls.Client(client, cfg).HandshakeContext(t.Context())
	}()

	require.NoError(t, server.SetReadDeadline(time.Now().Add(5*time.Second)))

	buf := make([]byte, 64<<10)
	n, err := server.Read(buf)
	require.NoError(t, err)

	return buf[:n]
}

// TestTLSStreamRealClientHello parses crypto/tls's own ClientHello, which
// carries a post-quantum key share and so exceeds one 1280-byte tunnel
// packet, delivered in the segment sizes a tailnet path produces and one
// byte at a time.
func TestTLSStreamRealClientHello(t *testing.T) {
	record := captureClientHello(t, &tls.Config{ServerName: "Files.Example.COM", MinVersion: tls.VersionTLS12})
	require.True(t, LooksLikeTLS(record))
	require.Greater(t, len(record), 1240, "expected a ClientHello larger than one tunnel segment")

	for _, size := range []int{1240, 1, 700} {
		var stream TLSStream

		var (
			hello Hello
			done  bool
		)

		for off := 0; off < len(record); off += size {
			require.False(t, done, "complete before the last segment")

			var err error

			hello, done, err = stream.Write(record[off:min(off+size, len(record))])
			require.NoError(t, err)
		}

		require.True(t, done, "segment size %d", size)
		assert.Equal(t, Hello{ServerName: "files.example.com"}, hello)
	}
}

func TestTLSStreamWithoutServerName(t *testing.T) {
	record := captureClientHello(
		t,
		&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
	)

	var stream TLSStream

	hello, done, err := stream.Write(record)
	require.NoError(t, err)
	require.True(t, done)
	assert.Equal(t, Hello{}, hello)
}

// echConfigList builds an ECHConfigList (draft-ietf-tls-esni-22 §4) for a
// fresh X25519 key, so crypto/tls sends a real Encrypted Client Hello.
func echConfigList(t *testing.T, publicName string) []byte {
	t.Helper()

	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)

	var config cryptobyte.Builder

	config.AddUint16(0xfe0d)
	config.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint8(1)       // the configuration id
		b.AddUint16(0x0020) // the X25519 KEM with HKDF-SHA256
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(key.PublicKey().Bytes()) })
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddUint16(0x0001) // HKDF-SHA256
			b.AddUint16(0x0001) // AES-128-GCM
		})
		b.AddUint8(0) // maximum_name_length
		b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes([]byte(publicName)) })
		b.AddUint16(0) // extensions
	})

	var list cryptobyte.Builder

	list.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(config.BytesOrPanic()) })

	return list.BytesOrPanic()
}

// TestTLSStreamEncryptedClientHello shows that with ECH the readable name
// is the provider's public name, and that the hello says so.
func TestTLSStreamEncryptedClientHello(t *testing.T) {
	record := captureClientHello(t, &tls.Config{
		ServerName:                     "secret.example.com",
		MinVersion:                     tls.VersionTLS13,
		EncryptedClientHelloConfigList: echConfigList(t, "public.example.net"),
	})

	var stream TLSStream

	hello, done, err := stream.Write(record)
	require.NoError(t, err)
	require.True(t, done)
	assert.Equal(t, Hello{ServerName: "public.example.net", ECH: true}, hello)
}

func TestTLSStreamRejects(t *testing.T) {
	valid := captureClientHello(t, &tls.Config{ServerName: "example.com", MinVersion: tls.VersionTLS12})

	badName := bytes.Clone(valid)
	idx := bytes.Index(badName, []byte("example.com"))
	require.Positive(t, idx)
	badName[idx] = 0x07

	serverHello := append([]byte(nil), valid...)
	serverHello[recordHeaderLen] = 2

	for _, tc := range []struct {
		name string
		in   []byte
		want error
	}{
		{name: "http", in: []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"), want: ErrNotTLS},
		{name: "empty record", in: []byte{0x16, 0x03, 0x01, 0x00, 0x00}, want: ErrMalformed},
		{name: "oversized record", in: []byte{0x16, 0x03, 0x01, 0xff, 0xff}, want: ErrMalformed},
		{name: "control byte in the name", in: badName, want: ErrMalformed},
		{name: "not a client hello", in: serverHello, want: ErrNotClientHello},
		{
			name: "hello longer than the limit",
			in:   []byte{0x16, 0x03, 0x01, 0x00, 0x04, 0x01, 0x01, 0x00, 0x00},
			want: ErrTooLarge,
		},
		{
			name: "extensions cut short",
			in: helloRecord(
				[]byte{0x03, 0x03}, 32, []byte{0}, []byte{0, 2, 0x13, 0x01}, []byte{1, 0}, []byte{0, 9, 0, 0},
			),
			want: ErrMalformed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stream TLSStream

			_, done, err := stream.Write(tc.in)
			require.ErrorIs(t, err, tc.want)
			require.False(t, done)
		})
	}

	t.Run("endless records", func(t *testing.T) {
		var stream TLSStream

		// A stream of small records that never completes a hello stops
		// being buffered.
		chunk := []byte{0x16, 0x03, 0x01, 0x00, 0x04, 0x01, 0x00, 0x3f, 0xfb}

		var err error
		for range MaxHelloSize {
			_, _, err = stream.Write(chunk)
			if err != nil {
				break
			}
		}

		require.Error(t, err)
	})
}

// helloRecord builds a ClientHello record from its raw parts, for inputs
// crypto/tls would never produce.
func helloRecord(version []byte, randomLen int, sessionID, suites, compression, rest []byte) []byte {
	body := append([]byte(nil), version...)
	body = append(body, make([]byte, randomLen)...)
	body = append(body, sessionID...)
	body = append(body, suites...)
	body = append(body, compression...)
	body = append(body, rest...)

	msg := append([]byte{1, 0, byte(len(body) >> 8), byte(len(body))}, body...)

	return append([]byte{0x16, 0x03, 0x01, byte(len(msg) >> 8), byte(len(msg))}, msg...)
}

// TestQUICRealClientInitial captures the Initial datagrams of a real QUIC
// client (golang.org/x/net/quic over crypto/tls) and reads its server name
// from them.
func TestQUICRealClientInitial(t *testing.T) {
	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { sink.Close() })

	endpoint, err := quic.Listen("udp", "127.0.0.1:0", nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()

		_ = endpoint.Close(t.Context())
	})

	go func() {
		_, _ = endpoint.Dial(ctx, "udp", sink.LocalAddr().String(), &quic.Config{
			TLSConfig: &tls.Config{
				ServerName: "quic.example.org",
				MinVersion: tls.VersionTLS13,
				NextProtos: []string{"h3"},
			},
		})
	}()

	var stream CryptoStream

	buf := make([]byte, 65535)

	for {
		require.NoError(t, sink.SetReadDeadline(time.Now().Add(5*time.Second)))

		n, _, err := sink.ReadFromUDP(buf)
		require.NoError(t, err)

		initial, err := ParseInitials(buf[:n])
		if errors.Is(err, ErrNotInitial) {
			continue
		}

		require.NoError(t, err)

		hello, done, err := stream.Add(initial.Frames)
		require.NoError(t, err)

		if done {
			assert.Equal(t, "quic.example.org", hello.ServerName)

			return
		}
	}
}

func FuzzParseInitials(f *testing.F) {
	raw, err := os.ReadFile("testdata/rfc9001-client-initial.hex")
	if err != nil {
		f.Fatal(err)
	}

	seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		f.Fatal(err)
	}

	f.Add(seed)
	f.Add(seed[:64])

	f.Fuzz(func(_ *testing.T, b []byte) {
		initial, err := ParseInitials(b)
		if err != nil {
			return
		}

		var stream CryptoStream

		_, _, _ = stream.Add(initial.Frames)
	})
}

func FuzzTLSStream(f *testing.F) {
	f.Add(helloRecord([]byte{3, 3}, 32, []byte{0}, []byte{0, 2, 0x13, 1}, []byte{1, 0}, []byte{0, 0}))
	f.Add([]byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x01, 0x00})

	f.Fuzz(func(_ *testing.T, b []byte) {
		var stream TLSStream

		_, _, _ = stream.Write(b)
	})
}
