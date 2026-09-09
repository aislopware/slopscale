package state

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// attestationSigner is what a client with a TPM-backed key does on every
// map request: sign the SHA-256 of "<unix seconds>|<node key>".
func attestationSigner(tb testing.TB) (*ecdsa.PrivateKey, key.HardwareAttestationPublic) {
	tb.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(tb, err)

	raw, err := priv.PublicKey.Bytes()
	require.NoError(tb, err)

	var pub key.HardwareAttestationPublic

	require.NoError(tb, pub.UnmarshalText([]byte("hwattestpub:"+hex.EncodeToString(raw))))

	return priv, pub
}

func signAttestation(tb testing.TB, priv *ecdsa.PrivateKey, at time.Time, nodeKey key.NodePublic) []byte {
	tb.Helper()

	digest := sha256.Sum256(fmt.Appendf(nil, "%d|%s", at.Unix(), nodeKey.String()))

	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	require.NoError(tb, err)

	return sig
}

func TestVerifyHardwareAttestation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	nodeKey := key.NewNode().Public()
	other := key.NewNode().Public()
	priv, pub := attestationSigner(t)

	tests := []struct {
		name    string
		req     tailcfg.MapRequest
		nodeKey key.NodePublic
		now     time.Time
		want    bool
	}{
		{
			name: "a valid signature attests",
			req: tailcfg.MapRequest{
				HardwareAttestationKey:                   pub,
				HardwareAttestationKeySignature:          signAttestation(t, priv, now, nodeKey),
				HardwareAttestationKeySignatureTimestamp: now,
			},
			nodeKey: nodeKey,
			now:     now,
			want:    true,
		},
		{
			name:    "a request without a key does not",
			req:     tailcfg.MapRequest{},
			nodeKey: nodeKey,
			now:     now,
			want:    false,
		},
		{
			name: "a key without a signature does not",
			req: tailcfg.MapRequest{
				HardwareAttestationKey:                   pub,
				HardwareAttestationKeySignatureTimestamp: now,
			},
			nodeKey: nodeKey,
			now:     now,
			want:    false,
		},
		{
			name: "a signature over another node key does not",
			req: tailcfg.MapRequest{
				HardwareAttestationKey:                   pub,
				HardwareAttestationKeySignature:          signAttestation(t, priv, now, other),
				HardwareAttestationKeySignatureTimestamp: now,
			},
			nodeKey: nodeKey,
			now:     now,
			want:    false,
		},
		{
			name: "a forged signature does not",
			req: tailcfg.MapRequest{
				HardwareAttestationKey:                   pub,
				HardwareAttestationKeySignature:          make([]byte, 64),
				HardwareAttestationKeySignatureTimestamp: now,
			},
			nodeKey: nodeKey,
			now:     now,
			want:    false,
		},
		{
			name: "a signature from within the day does",
			req: tailcfg.MapRequest{
				HardwareAttestationKey:                   pub,
				HardwareAttestationKeySignature:          signAttestation(t, priv, now.Add(-23*time.Hour), nodeKey),
				HardwareAttestationKeySignatureTimestamp: now.Add(-23 * time.Hour),
			},
			nodeKey: nodeKey,
			now:     now,
			want:    true,
		},
		{
			name: "a signature older than a day does not",
			req: tailcfg.MapRequest{
				HardwareAttestationKey:                   pub,
				HardwareAttestationKeySignature:          signAttestation(t, priv, now.Add(-25*time.Hour), nodeKey),
				HardwareAttestationKeySignatureTimestamp: now.Add(-25 * time.Hour),
			},
			nodeKey: nodeKey,
			now:     now,
			want:    false,
		},
		{
			name: "a signature more than a day ahead does not",
			req: tailcfg.MapRequest{
				HardwareAttestationKey:                   pub,
				HardwareAttestationKeySignature:          signAttestation(t, priv, now.Add(25*time.Hour), nodeKey),
				HardwareAttestationKeySignatureTimestamp: now.Add(25 * time.Hour),
			},
			nodeKey: nodeKey,
			now:     now,
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := verifyHardwareAttestation(tc.req, tc.nodeKey, tc.now)
			assert.Equal(t, tc.want, ok)

			if tc.want {
				assert.True(t, got.Equal(pub))
			} else {
				assert.True(t, got.IsZero())
			}
		})
	}
}

func TestApplyHardwareAttestation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	_, first := attestationSigner(t)
	_, second := attestationSigner(t)

	t.Run("the first valid signature attests", func(t *testing.T) {
		t.Parallel()

		node := &types.Node{}

		move := applyHardwareAttestation(node, first, true, now)
		assert.True(t, move.gained)
		assert.False(t, move.keyChanged)
		require.NotNil(t, node.HardwareAttestation)
		assert.True(t, node.HardwareAttestation.Attested)
		assert.Equal(t, now, node.HardwareAttestation.AttestedAt)
		assert.True(t, node.HardwareAttestation.KeyChangedAt.IsZero())
	})

	t.Run("a repeat of the same state changes nothing", func(t *testing.T) {
		t.Parallel()

		node := &types.Node{}
		applyHardwareAttestation(node, first, true, now)
		stored := node.HardwareAttestation

		move := applyHardwareAttestation(node, first, true, later)
		assert.False(t, move.moved())
		assert.Same(t, stored, node.HardwareAttestation, "the record is untouched")
		assert.Equal(t, now, node.HardwareAttestation.AttestedAt, "AttestedAt is a transition time")
	})

	t.Run("a request without a signature loses attestation once", func(t *testing.T) {
		t.Parallel()

		node := &types.Node{}
		applyHardwareAttestation(node, first, true, now)

		move := applyHardwareAttestation(node, key.HardwareAttestationPublic{}, false, later)
		assert.True(t, move.lost)
		assert.False(t, node.HardwareAttestation.Attested)
		assert.True(t, node.HardwareAttestation.Key.Equal(first), "the last key that verified stays")

		move = applyHardwareAttestation(node, key.HardwareAttestationPublic{}, false, later)
		assert.False(t, move.moved(), "a node that is already not attested does not move")
	})

	t.Run("a new key replaces the old one and is recorded", func(t *testing.T) {
		t.Parallel()

		node := &types.Node{}
		applyHardwareAttestation(node, first, true, now)

		move := applyHardwareAttestation(node, second, true, later)
		assert.True(t, move.keyChanged)
		assert.False(t, move.gained)
		assert.True(t, node.HardwareAttestation.Key.Equal(second))
		assert.Equal(t, later, node.HardwareAttestation.KeyChangedAt)
		assert.Equal(t, now, node.HardwareAttestation.AttestedAt)
	})

	t.Run("a node that never attested does not move", func(t *testing.T) {
		t.Parallel()

		node := &types.Node{}

		move := applyHardwareAttestation(node, key.HardwareAttestationPublic{}, false, now)
		assert.False(t, move.moved())
		assert.Nil(t, node.HardwareAttestation)
	})
}
