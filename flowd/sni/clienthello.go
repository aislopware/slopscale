// Package sni reads the server name a client asks for from the first bytes
// of a TLS connection (a ClientHello in TLS records over TCP) or a QUIC
// connection (a ClientHello in CRYPTO frames of the client's Initial
// packets, whose protection keys are derived from public values).
//
// Every parser here reads attacker-controlled input from the network, so
// each one bounds what it buffers and rejects rather than guesses.
package sni

import (
	"errors"
	"strings"

	"golang.org/x/crypto/cryptobyte"
)

// MaxHelloSize bounds a ClientHello handshake message. Real ones are under
// 2 KiB (about 1.8 KiB with a post-quantum key share); anything larger is
// not worth buffering.
const MaxHelloSize = 16 << 10

const (
	handshakeClientHello = 1
	handshakeHeaderLen   = 4
	extServerName        = 0x0000
	extECH               = 0xfe0d
	serverNameHost       = 0
	maxHostnameLen       = 253
	randomLen            = 32
)

var (
	// ErrNotClientHello is returned for bytes that do not start a
	// ClientHello.
	ErrNotClientHello = errors.New("not a TLS ClientHello")
	// ErrMalformed is returned for a ClientHello that does not parse.
	ErrMalformed = errors.New("malformed ClientHello")
	// ErrTooLarge is returned for a ClientHello over [MaxHelloSize].
	ErrTooLarge = errors.New("ClientHello too large")
)

// Hello is what a ClientHello says about the server it is for.
type Hello struct {
	// ServerName is the server_name extension's host name, lower case;
	// empty when the client sent none. With ECH it is the outer, public
	// name, not the one the client connects to.
	ServerName string
	// ECH is whether the ClientHello carries the Encrypted Client Hello
	// extension.
	ECH bool
}

// helloLength returns the full length of the handshake message msg starts,
// header included, once msg holds the header.
func helloLength(msg []byte) (int, bool, error) {
	if len(msg) < handshakeHeaderLen {
		return 0, false, nil
	}

	if msg[0] != handshakeClientHello {
		return 0, false, ErrNotClientHello
	}

	n := handshakeHeaderLen + (int(msg[1])<<16 | int(msg[2])<<8 | int(msg[3]))
	if n > MaxHelloSize {
		return 0, false, ErrTooLarge
	}

	return n, true, nil
}

// ParseClientHello parses a complete ClientHello handshake message,
// starting at its four-byte handshake header.
func ParseClientHello(msg []byte) (Hello, error) {
	n, ok, err := helloLength(msg)
	if err != nil {
		return Hello{}, err
	}

	if !ok || len(msg) < n {
		return Hello{}, ErrMalformed
	}

	body := cryptobyte.String(msg[handshakeHeaderLen:n])

	var (
		legacyVersion uint16
		sessionID     cryptobyte.String
		cipherSuites  cryptobyte.String
		compression   cryptobyte.String
		extensions    cryptobyte.String
	)

	if !body.ReadUint16(&legacyVersion) ||
		!body.Skip(randomLen) ||
		!body.ReadUint8LengthPrefixed(&sessionID) ||
		!body.ReadUint16LengthPrefixed(&cipherSuites) ||
		!body.ReadUint8LengthPrefixed(&compression) {
		return Hello{}, ErrMalformed
	}

	// A ClientHello without extensions is valid TLS 1.2 and names no
	// server.
	if body.Empty() {
		return Hello{}, nil
	}

	if !body.ReadUint16LengthPrefixed(&extensions) || !body.Empty() {
		return Hello{}, ErrMalformed
	}

	return parseExtensions(extensions)
}

func parseExtensions(extensions cryptobyte.String) (Hello, error) {
	var hello Hello

	for !extensions.Empty() {
		var (
			typ  uint16
			data cryptobyte.String
		)

		if !extensions.ReadUint16(&typ) || !extensions.ReadUint16LengthPrefixed(&data) {
			return Hello{}, ErrMalformed
		}

		switch typ {
		case extServerName:
			name, err := parseServerName(data)
			if err != nil {
				return Hello{}, err
			}

			hello.ServerName = name
		case extECH:
			hello.ECH = true
		}
	}

	return hello, nil
}

func parseServerName(data cryptobyte.String) (string, error) {
	var list cryptobyte.String
	if !data.ReadUint16LengthPrefixed(&list) || !data.Empty() {
		return "", ErrMalformed
	}

	for !list.Empty() {
		var (
			nameType uint8
			name     cryptobyte.String
		)

		if !list.ReadUint8(&nameType) || !list.ReadUint16LengthPrefixed(&name) {
			return "", ErrMalformed
		}

		if nameType != serverNameHost {
			continue
		}

		host := strings.TrimSuffix(strings.ToLower(string(name)), ".")
		if !validHostname(host) {
			return "", ErrMalformed
		}

		return host, nil
	}

	return "", nil
}

// validHostname accepts the bytes a DNS host name may hold, so a crafted
// name cannot smuggle control characters into the report or the console.
func validHostname(host string) bool {
	if host == "" || len(host) > maxHostnameLen {
		return false
	}

	for _, c := range []byte(host) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '.', c == '_':
		default:
			return false
		}
	}

	return true
}
