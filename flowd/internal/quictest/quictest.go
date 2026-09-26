// Package quictest builds protected QUIC v1 client Initial packets, the way
// any client (or anyone posing as one) can, since their keys come from
// public values (RFC 9001 §5.2).
package quictest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"slices"

	"golang.org/x/crypto/cryptobyte"
)

const (
	secretLen   = 32
	keyLen      = 16
	ivLen       = 12
	pnLen       = 4
	firstByte   = 0xc0 | (pnLen - 1) // long header, fixed bit, Initial
	hpMask      = 0x0f
	cryptoFrame = 0x06
	// varint8 marks an eight-byte variable-length integer.
	varint8 = 0xc0 << 56
)

var saltV1 = []byte{
	0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17,
	0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a,
}

// Initial returns a QUIC v1 client Initial for dcid with packet number pn,
// protecting payload (frames, unpadded).
func Initial(dcid []byte, pn uint32, payload []byte) []byte {
	secret, err := hkdf.Extract(sha256.New, dcid, saltV1)
	if err != nil {
		panic(err)
	}

	client := expand(secret, "client in", secretLen)

	block, err := aes.NewCipher(expand(client, "quic key", keyLen))
	if err != nil {
		panic(err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}

	hp, err := aes.NewCipher(expand(client, "quic hp", keyLen))
	if err != nil {
		panic(err)
	}

	// Version 1, no source connection ID, no token.
	var header cryptobyte.Builder

	header.AddUint8(firstByte)
	header.AddUint32(1)
	header.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(dcid) })
	header.AddUint8(0)
	header.AddUint8(0)

	hdr := appendVarint(header.BytesOrPanic(), pnLen+len(payload)+aead.Overhead())

	pnAt := len(hdr)
	hdr = binary.BigEndian.AppendUint32(hdr, pn)

	nonce := expand(client, "quic iv", ivLen)
	for i := range pnLen {
		nonce[ivLen-pnLen+i] ^= hdr[pnAt+i]
	}

	pkt := aead.Seal(slices.Clone(hdr), nonce, payload, hdr)

	var mask [aes.BlockSize]byte
	hp.Encrypt(mask[:], pkt[pnAt+pnLen:pnAt+pnLen+aes.BlockSize])

	pkt[0] ^= mask[0] & hpMask
	for i := range pnLen {
		pkt[pnAt+i] ^= mask[1+i]
	}

	return pkt
}

// Crypto returns a CRYPTO frame carrying data at offset.
func Crypto(offset int, data []byte) []byte {
	frame := appendVarint([]byte{cryptoFrame}, offset)
	frame = appendVarint(frame, len(data))

	return append(frame, data...)
}

// appendVarint appends v in the eight-byte form, which any reader must
// accept for any value.
func appendVarint(b []byte, v int) []byte {
	//nolint:gosec // fixture sizes, never negative
	return binary.BigEndian.AppendUint64(b, varint8|uint64(v))
}

// expand is TLS 1.3's HKDF-Expand-Label with an empty context.
func expand(secret []byte, label string, n int) []byte {
	var info cryptobyte.Builder

	//nolint:gosec // one of the key sizes above
	info.AddUint16(uint16(n))
	info.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes([]byte("tls13 " + label)) })
	info.AddUint8(0)

	out, err := hkdf.Expand(sha256.New, secret, string(info.BytesOrPanic()), n)
	if err != nil {
		panic(err)
	}

	return out
}
