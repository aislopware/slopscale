package servertest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"tailscale.com/types/key"
)

// softwareAttestationKey stands in for the TPM-backed key a client built
// with feature/tpm signs its map requests with. The harness cannot reach
// a TPM, and tailscale.com/feature/tpm is not linked into this package,
// so the platform hooks are free for a key that lives in memory. It is a
// P-256 key like the real one, so the server's verification path is the
// one production uses.
type softwareAttestationKey struct {
	priv *ecdsa.PrivateKey
	// sign, when set, replaces the signature the key would make, which
	// is how a test presents a forgery under a genuine key.
	sign func(digest []byte) []byte
}

// registerAttestationKeys installs the platform hooks once per process;
// tailscale.com/types/key panics on a second registration.
var registerAttestationKeys sync.Once

// newSoftwareAttestationKey returns a fresh in-memory attestation key.
// A non-nil sign replaces the signature it makes.
func newSoftwareAttestationKey(sign func(digest []byte) []byte) *softwareAttestationKey {
	registerAttestationKeys.Do(func() {
		key.RegisterHardwareAttestationKeyFns(
			func() key.HardwareAttestationKey { return &softwareAttestationKey{} },
			func() (key.HardwareAttestationKey, error) { return newSoftwareAttestationKey(nil), nil },
		)
	})

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("servertest: generating attestation key: %v", err))
	}

	return &softwareAttestationKey{priv: priv, sign: sign}
}

func (k *softwareAttestationKey) Public() crypto.PublicKey {
	return &k.priv.PublicKey
}

func (k *softwareAttestationKey) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	if k.sign != nil {
		return k.sign(digest), nil
	}

	return ecdsa.SignASN1(rand.Reader, k.priv, digest)
}

// MarshalJSON encodes the raw private key, which is all it takes to
// rebuild it; the real keys never leave their TPM and encode a handle.
func (k *softwareAttestationKey) MarshalJSON() ([]byte, error) {
	if k.IsZero() {
		return json.Marshal("")
	}

	raw, err := k.priv.Bytes()
	if err != nil {
		return nil, err
	}

	return json.Marshal(hex.EncodeToString(raw))
}

func (k *softwareAttestationKey) UnmarshalJSON(b []byte) error {
	var encoded string

	err := json.Unmarshal(b, &encoded)
	if err != nil {
		return err
	}

	if encoded == "" {
		k.priv = nil

		return nil
	}

	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return err
	}

	k.priv, err = ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)

	return err
}

func (k *softwareAttestationKey) Close() error { return nil }

//nolint:ireturn // the interface this implements returns itself
func (k *softwareAttestationKey) Clone() key.HardwareAttestationKey {
	if k == nil {
		return nil
	}

	clone := *k

	return &clone
}

func (k *softwareAttestationKey) IsZero() bool { return k == nil || k.priv == nil }

// WithHardwareAttestation makes the client sign every map request with an
// attestation key, as a Linux or Windows client started with
// tailscaled --hardware-attestation does.
func WithHardwareAttestation() ClientOption {
	return func(c *clientConfig) { c.attestationKey = newSoftwareAttestationKey(nil) }
}

// WithHardwareAttestationSigner is [WithHardwareAttestation] with the
// signature replaced by what sign returns, so a test can present a
// forgery under a key the server has never rejected.
func WithHardwareAttestationSigner(sign func(digest []byte) []byte) ClientOption {
	return func(c *clientConfig) { c.attestationKey = newSoftwareAttestationKey(sign) }
}
