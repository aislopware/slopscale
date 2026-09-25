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
// in principle, over several handshake records. What follows it (a
// ChangeCipherSpec, early data) is never read.
type TLSStream struct {
	record    []byte // the record being read, header included
	handshake []byte // handshake message bytes collected from records
}

// Write adds the next in-order payload bytes. It returns done once the
// ClientHello is complete (with the parsed result) or an error when the
// stream is not a ClientHello; the stream must not be written to after
// either.
func (s *TLSStream) Write(p []byte) (Hello, bool, error) {
	for len(p) > 0 {
		want := recordHeaderLen
		if len(s.record) >= recordHeaderLen {
			want += int(binary.BigEndian.Uint16(s.record[3:recordHeaderLen]))
		}

		n := min(want-len(s.record), len(p))
		s.record = append(s.record, p[:n]...)
		p = p[n:]

		if len(s.record) == recordHeaderLen {
			err := checkRecordHeader(s.record)
			if err != nil {
				return Hello{}, false, err
			}

			continue
		}

		if len(s.record) < want {
			break
		}

		s.handshake = append(s.handshake, s.record[recordHeaderLen:]...)
		s.record = s.record[:0]

		total, ok, err := helloLength(s.handshake)
		if err != nil {
			return Hello{}, false, err
		}

		if ok && len(s.handshake) >= total {
			hello, err := ParseClientHello(s.handshake[:total])
			if err != nil {
				return Hello{}, false, err
			}

			return hello, true, nil
		}
	}

	return Hello{}, false, nil
}

// Held is how many bytes the stream buffers.
func (s *TLSStream) Held() int {
	return cap(s.record) + cap(s.handshake)
}

func checkRecordHeader(h []byte) error {
	if h[0] != recordTypeHandshake || h[1] != recordVersionMajor {
		return ErrNotTLS
	}

	n := int(binary.BigEndian.Uint16(h[3:recordHeaderLen]))
	if n == 0 || n > maxRecordLen {
		return ErrMalformed
	}

	return nil
}
