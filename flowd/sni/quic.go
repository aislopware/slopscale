package sni

import (
	"bytes"
	"cmp"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"golang.org/x/crypto/cryptobyte"
)

// QUIC versions whose Initial packets are read. Both protect Initials with
// keys derived from the client's Destination Connection ID and a public
// salt, so a middlebox can read the ClientHello (RFC 9001 §5.2, RFC 9369
// §3.3.1).
const (
	quicV1 uint32 = 0x00000001
	quicV2 uint32 = 0x6b3343cf
)

const (
	headerFormLong = 0x80
	headerFixedBit = 0x40
	// longTypeMask selects the two long-header packet type bits.
	longTypeMask  = 0x30
	longTypeShift = 4
	initialTypeV1 = 0
	initialTypeV2 = 1
	retryTypeV1   = 3
	retryTypeV2   = 0
	pnLenMask     = 0x03
	longHPMask    = 0x0f
	maxConnIDLen  = 20
	hpSampleLen   = 16
	maxPNLen      = 4
	aeadKeyLen    = 16
	aeadIVLen     = 12
	aeadTagLen    = 16
	initialSecret = 32
	bitsPerByte   = 8
	maxDatagram   = 1 << 16

	frameTypePadding         = 0x00
	frameTypePing            = 0x01
	frameTypeAck             = 0x02
	frameTypeAckECN          = 0x03
	frameTypeCrypto          = 0x06
	frameTypeConnCloseQUIC   = 0x1c
	frameTypeConnCloseApp    = 0x1d
	varintLenMask            = 0xc0
	varintValueMask          = 0x3f
	varintLenShift           = 6
	maxCryptoOffset          = MaxHelloSize
	maxCryptoRanges          = 16
	ackRangeFieldsPerAckPair = 2
	ecnCounts                = 3
)

var (
	saltV1 = []byte{
		0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17,
		0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a,
	}
	saltV2 = []byte{
		0x0d, 0xed, 0xe3, 0xde, 0xf7, 0x00, 0xa6, 0xdb, 0x81, 0x93,
		0x81, 0xbe, 0x6e, 0x26, 0x9d, 0xcb, 0xf9, 0xbd, 0x2e, 0xd9,
	}
)

var (
	// ErrNotInitial is returned for a datagram that does not start with a
	// client Initial packet of a supported QUIC version.
	ErrNotInitial = errors.New("not a QUIC Initial packet")
	// ErrQUICMalformed is returned for an Initial that does not parse or
	// does not decrypt.
	ErrQUICMalformed = errors.New("malformed QUIC Initial packet")
	// ErrFragmented is returned for CRYPTO frames that leave more gaps
	// than a client's reordering explains.
	ErrFragmented = errors.New("CRYPTO stream too fragmented")
)

// CryptoFrame is a CRYPTO frame's data at its offset in the stream.
type CryptoFrame struct {
	Offset uint64
	Data   []byte
}

// Initial is what the client Initial packets of one UDP datagram carry.
type Initial struct {
	// DCID is the Destination Connection ID the client chose; with the
	// addresses it identifies the connection until the server answers.
	DCID []byte
	// Frames are the CRYPTO frames of every Initial in the datagram.
	Frames []CryptoFrame
}

// LooksLikeQUICInitial reports whether a UDP payload may start with a QUIC
// long-header packet, as a cheap check before [ParseInitials].
func LooksLikeQUICInitial(payload []byte) bool {
	return len(payload) > 0 && payload[0]&(headerFormLong|headerFixedBit) == headerFormLong|headerFixedBit
}

// ParseInitials decrypts the client Initial packets coalesced in a UDP
// datagram and returns their CRYPTO frames. Packets of other types in the
// datagram are skipped; the datagram must start with an Initial.
func ParseInitials(datagram []byte) (Initial, error) {
	var (
		out         Initial
		rest        = datagram
		keys        initialKeys
		keysVersion uint32
	)

	for len(rest) > 0 && rest[0]&headerFormLong != 0 {
		pkt, next, err := parseLongPacket(rest)
		if err != nil {
			if out.DCID == nil {
				return Initial{}, err
			}

			break
		}

		rest = next

		if !pkt.initial {
			continue
		}

		if out.DCID == nil {
			out.DCID = pkt.dcid
		} else if !bytes.Equal(out.DCID, pkt.dcid) {
			break
		}

		// Coalesced Initials share the connection ID, so their keys.
		if keys.aead == nil || keysVersion != pkt.version {
			keys, err = deriveKeys(pkt.version, pkt.dcid)
			if err != nil {
				return Initial{}, err
			}

			keysVersion = pkt.version
		}

		frames, err := decryptInitial(pkt, keys)
		if err != nil {
			return Initial{}, err
		}

		out.Frames = append(out.Frames, frames...)
	}

	if out.DCID == nil {
		return Initial{}, ErrNotInitial
	}

	return out, nil
}

// longPacket is one long-header packet as it is on the wire.
type longPacket struct {
	version  uint32
	initial  bool
	dcid     []byte
	packet   []byte // the whole packet, header included
	pnOffset int
}

func parseLongPacket(b []byte) (longPacket, []byte, error) {
	const versionOffset, versionLen = 1, 4

	if len(b) < versionOffset+versionLen+1 || b[0]&headerFixedBit == 0 {
		return longPacket{}, nil, ErrNotInitial
	}

	pkt := longPacket{version: binary.BigEndian.Uint32(b[versionOffset:])}

	typ := (b[0] & longTypeMask) >> longTypeShift

	switch pkt.version {
	case quicV1:
		pkt.initial = typ == initialTypeV1
		if typ == retryTypeV1 {
			return longPacket{}, nil, ErrNotInitial
		}
	case quicV2:
		pkt.initial = typ == initialTypeV2
		if typ == retryTypeV2 {
			return longPacket{}, nil, ErrNotInitial
		}
	default:
		return longPacket{}, nil, ErrNotInitial
	}

	off := versionOffset + versionLen

	dcid, off, ok := readConnID(b, off)
	if !ok {
		return longPacket{}, nil, ErrQUICMalformed
	}

	_, off, ok = readConnID(b, off)
	if !ok {
		return longPacket{}, nil, ErrQUICMalformed
	}

	if pkt.initial {
		tokenLen, n, fits := readLength(b[off:])
		if !fits {
			return longPacket{}, nil, ErrQUICMalformed
		}

		off += n + tokenLen
	}

	length, n, ok := readLength(b[off:])
	if !ok {
		return longPacket{}, nil, ErrQUICMalformed
	}

	off += n
	end := off + length

	pkt.dcid = dcid
	pkt.packet = b[:end]
	pkt.pnOffset = off

	return pkt, b[end:], nil
}

func readConnID(b []byte, off int) ([]byte, int, bool) {
	if off >= len(b) {
		return nil, 0, false
	}

	n := int(b[off])
	off++

	if n > maxConnIDLen || off+n > len(b) {
		return nil, 0, false
	}

	return slices.Clone(b[off : off+n]), off + n, true
}

// readVarint decodes a QUIC variable-length integer (RFC 9000 §16).
func readVarint(b []byte) (uint64, int, bool) {
	if len(b) == 0 {
		return 0, 0, false
	}

	n := 1 << ((b[0] & varintLenMask) >> varintLenShift)
	if len(b) < n {
		return 0, 0, false
	}

	v := uint64(b[0] & varintValueMask)
	for _, c := range b[1:n] {
		v = v<<bitsPerByte | uint64(c)
	}

	return v, n, true
}

// readLength reads a varint length and checks that many bytes follow
// it in b.
func readLength(b []byte) (int, int, bool) {
	v, n, ok := readVarint(b)
	if !ok || v > maxDatagram {
		return 0, 0, false
	}

	length := int(v)
	if length > len(b)-n {
		return 0, 0, false
	}

	return length, n, true
}

// initialKeys are the client's Initial packet protection keys.
type initialKeys struct {
	aead cipher.AEAD
	iv   []byte
	hp   cipher.Block
}

// deriveKeys is [deriveInitialKeys]; tests count its calls.
var deriveKeys = deriveInitialKeys

func deriveInitialKeys(version uint32, dcid []byte) (initialKeys, error) {
	salt, labelPrefix := saltV1, "quic "
	if version == quicV2 {
		salt, labelPrefix = saltV2, "quicv2 "
	}

	secret, err := hkdf.Extract(sha256.New, dcid, salt)
	if err != nil {
		return initialKeys{}, fmt.Errorf("extracting the initial secret: %w", err)
	}

	client, err := expandLabel(secret, "client in", initialSecret)
	if err != nil {
		return initialKeys{}, err
	}

	key, err := expandLabel(client, labelPrefix+"key", aeadKeyLen)
	if err != nil {
		return initialKeys{}, err
	}

	iv, err := expandLabel(client, labelPrefix+"iv", aeadIVLen)
	if err != nil {
		return initialKeys{}, err
	}

	hpKey, err := expandLabel(client, labelPrefix+"hp", aeadKeyLen)
	if err != nil {
		return initialKeys{}, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return initialKeys{}, fmt.Errorf("creating the packet cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return initialKeys{}, fmt.Errorf("creating the packet AEAD: %w", err)
	}

	hp, err := aes.NewCipher(hpKey)
	if err != nil {
		return initialKeys{}, fmt.Errorf("creating the header protection cipher: %w", err)
	}

	return initialKeys{aead: aead, iv: iv, hp: hp}, nil
}

// expandLabel is TLS 1.3's HKDF-Expand-Label with an empty context.
func expandLabel(secret []byte, label string, length uint16) ([]byte, error) {
	var info cryptobyte.Builder

	info.AddUint16(length)
	info.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes([]byte("tls13 " + label)) })
	info.AddUint8(0) // an empty context

	raw, err := info.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encoding the label %q: %w", label, err)
	}

	out, err := hkdf.Expand(sha256.New, secret, string(raw), int(length))
	if err != nil {
		return nil, fmt.Errorf("expanding %q: %w", label, err)
	}

	return out, nil
}

func decryptInitial(pkt longPacket, keys initialKeys) ([]CryptoFrame, error) {
	b := slices.Clone(pkt.packet)
	sampleAt := pkt.pnOffset + maxPNLen

	if len(b) < sampleAt+hpSampleLen {
		return nil, ErrQUICMalformed
	}

	var mask [aes.BlockSize]byte
	keys.hp.Encrypt(mask[:], b[sampleAt:sampleAt+hpSampleLen])

	b[0] ^= mask[0] & longHPMask
	pnLen := int(b[0]&pnLenMask) + 1
	payloadAt := pkt.pnOffset + pnLen

	// The packet number is unmasked in place; right-aligned, it is XORed
	// into the IV to make the nonce.
	pnBytes := b[pkt.pnOffset:payloadAt]
	for i := range pnBytes {
		pnBytes[i] ^= mask[1:][i]
	}

	if len(b)-payloadAt < aeadTagLen {
		return nil, ErrQUICMalformed
	}

	nonce := slices.Clone(keys.iv)
	for i, c := range pnBytes {
		nonce[len(nonce)-len(pnBytes)+i] ^= c
	}

	plain, err := keys.aead.Open(nil, nonce, b[payloadAt:], b[:payloadAt])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrQUICMalformed, err)
	}

	return cryptoFrames(plain)
}

// cryptoFrames returns the CRYPTO frames of a decrypted Initial payload.
// Only frames an Initial may carry are understood; parsing stops at any
// other, keeping what came before.
func cryptoFrames(payload []byte) ([]CryptoFrame, error) {
	var frames []CryptoFrame

	for len(payload) > 0 {
		typ, n, ok := readVarint(payload)
		if !ok {
			return frames, ErrQUICMalformed
		}

		payload = payload[n:]

		switch typ {
		case frameTypePadding, frameTypePing:
		case frameTypeCrypto:
			frame, rest, err := readCryptoFrame(payload)
			if err != nil {
				return frames, err
			}

			frames = append(frames, frame)
			payload = rest
		case frameTypeAck, frameTypeAckECN:
			rest, ok := skipAck(payload, typ == frameTypeAckECN)
			if !ok {
				return frames, ErrQUICMalformed
			}

			payload = rest
		default:
			// CONNECTION_CLOSE and anything unexpected end the useful
			// part of the packet.
			return frames, nil
		}
	}

	return frames, nil
}

func readCryptoFrame(b []byte) (CryptoFrame, []byte, error) {
	offset, n1, ok1 := readVarint(b)
	if !ok1 {
		return CryptoFrame{}, nil, ErrQUICMalformed
	}

	length, n2, ok2 := readLength(b[n1:])
	if !ok2 {
		return CryptoFrame{}, nil, ErrQUICMalformed
	}

	start := n1 + n2
	end := start + length

	return CryptoFrame{Offset: offset, Data: b[start:end]}, b[end:], nil
}

// skipAck skips an ACK frame's fields after its type.
func skipAck(b []byte, ecn bool) ([]byte, bool) {
	// Largest Acknowledged, ACK Delay, ACK Range Count, First ACK Range.
	var rangeCount uint64

	for i := range 4 {
		v, n, ok := readVarint(b)
		if !ok {
			return nil, false
		}

		if i == 2 {
			rangeCount = v
		}

		b = b[n:]
	}

	fields := rangeCount * ackRangeFieldsPerAckPair
	if ecn {
		fields += ecnCounts
	}

	if fields > uint64(len(b)) {
		return nil, false
	}

	for range fields {
		_, n, ok := readVarint(b)
		if !ok {
			return nil, false
		}

		b = b[n:]
	}

	return b, true
}

// CryptoStream reassembles the client's Initial CRYPTO stream, whose frames
// may arrive out of order and across datagrams (Chrome deliberately
// shuffles them), until the ClientHello at its start is complete.
type CryptoStream struct {
	buf    []byte
	ranges [][2]int // sorted, merged, half-open byte ranges held in buf
}

// Add adds frames and returns done once the ClientHello is complete, with
// the parsed result; or an error when the stream cannot be a ClientHello.
func (s *CryptoStream) Add(frames []CryptoFrame) (Hello, bool, error) {
	for _, f := range frames {
		if f.Offset > maxCryptoOffset {
			return Hello{}, false, ErrTooLarge
		}

		start := int(f.Offset)
		end := start + len(f.Data)

		if end > maxCryptoOffset {
			return Hello{}, false, ErrTooLarge
		}

		if start == end {
			continue
		}

		err := s.addRange(start, end)
		if err != nil {
			return Hello{}, false, err
		}

		if end > len(s.buf) {
			s.buf = append(s.buf, make([]byte, end-len(s.buf))...)
		}

		copy(s.buf[start:end], f.Data)
	}

	contiguous := s.contiguous()

	total, ok, err := helloLength(s.buf[:contiguous])
	if err != nil {
		return Hello{}, false, err
	}

	if !ok || contiguous < total {
		return Hello{}, false, nil
	}

	hello, err := ParseClientHello(s.buf[:total])
	if err != nil {
		return Hello{}, false, err
	}

	return hello, true, nil
}

// Held is how many bytes the stream buffers.
func (s *CryptoStream) Held() int {
	return cap(s.buf)
}

// addRange records that [start, end) is held, merging it with the ranges
// it overlaps or touches. A range that would be one more gap than
// [maxCryptoRanges] allows fails the stream: clients shuffle a handful of
// frames, not thousands.
func (s *CryptoStream) addRange(start, end int) error {
	// i is the first range ending at or after start; it and those after it
	// that begin at or before end merge with the new one.
	i, _ := slices.BinarySearchFunc(s.ranges, start, func(r [2]int, at int) int { return cmp.Compare(r[1], at) })

	j := i
	for j < len(s.ranges) && s.ranges[j][0] <= end {
		start, end = min(start, s.ranges[j][0]), max(end, s.ranges[j][1])
		j++
	}

	if i == j && len(s.ranges) >= maxCryptoRanges {
		return ErrFragmented
	}

	s.ranges = slices.Replace(s.ranges, i, j, [2]int{start, end})

	return nil
}

// contiguous is how many bytes from offset zero are held.
func (s *CryptoStream) contiguous() int {
	if len(s.ranges) == 0 || s.ranges[0][0] != 0 {
		return 0
	}

	return s.ranges[0][1]
}
