package sni

import (
	"encoding/binary"
	"errors"
)

const (
	recordHeaderLen     = 5
	recordTypeHandshake = 0x16
	recordVersionMajor  = 0x03
	// maxRecordLen is the largest TLSPlaintext fragment (2^14).
	maxRecordLen = 1 << 14
)

// ErrNotTLS is returned for a stream that does not start with a TLS
// handshake record.
var ErrNotTLS = errors.New("not a TLS handshake record")

// LooksLikeTLS reports whether payload may start a TLS connection: a
// handshake record of a TLS 1.x version.
func LooksLikeTLS(payload []byte) bool {
	return len(payload) >= 2 && payload[0] == recordTypeHandshake && payload[1] == recordVersionMajor
}

// TLSStream reassembles the ClientHello from the client's first bytes on a
// TCP connection. The ClientHello may be split over several segments and,
// in principle, over several handshake records.
type TLSStream struct {
	raw       []byte // record bytes not yet consumed
	handshake []byte // handshake message bytes collected from records
}

// Write adds the next in-order payload bytes. It returns done once the
// ClientHello is complete (with the parsed result) or an error when the
// stream is not a ClientHello; the stream must not be written to after
// either.
func (s *TLSStream) Write(p []byte) (Hello, bool, error) {
	if len(s.raw)+len(p) > MaxHelloSize+recordHeaderLen*4 {
		return Hello{}, false, ErrTooLarge
	}

	s.raw = append(s.raw, p...)

	for len(s.raw) >= recordHeaderLen {
		if s.raw[0] != recordTypeHandshake || s.raw[1] != recordVersionMajor {
			return Hello{}, false, ErrNotTLS
		}

		n := int(binary.BigEndian.Uint16(s.raw[3:recordHeaderLen]))
		if n == 0 || n > maxRecordLen {
			return Hello{}, false, ErrMalformed
		}

		if len(s.raw) < recordHeaderLen+n {
			break
		}

		s.handshake = append(s.handshake, s.raw[recordHeaderLen:recordHeaderLen+n]...)
		s.raw = s.raw[recordHeaderLen+n:]
	}

	total, ok, err := helloLength(s.handshake)
	if err != nil {
		return Hello{}, false, err
	}

	if !ok || len(s.handshake) < total {
		return Hello{}, false, nil
	}

	hello, err := ParseClientHello(s.handshake[:total])
	if err != nil {
		return Hello{}, false, err
	}

	return hello, true, nil
}
