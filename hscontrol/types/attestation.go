package types

import (
	"time"

	"tailscale.com/types/key"
)

// HardwareAttestation is what the server learned from the hardware
// attestation key a client signs its map requests with, see
// docs/ref/device-trust.md. The client generates the key inside the
// machine's TPM and cannot export it, so a valid signature says the map
// request came from the machine that first presented the key.
//
// Only the map request path writes it, through
// [State.UpdateNodeFromMapRequest], and only on a transition: a client
// that keeps attesting restates the same record on every request.
type HardwareAttestation struct {
	// Key is the last key that verified. It stays after attestation is
	// lost so an operator can see which key the machine used.
	Key key.HardwareAttestationPublic `json:"key"`

	// Attested says the node's last map request carried a valid
	// signature by Key over its current node key. It is what the
	// node:hardwareAttested posture attribute reports, so a machine that
	// stops signing loses the attribute on its next request.
	Attested bool `json:"attested"`

	// AttestedAt is when Attested last became true, not when the last
	// signature arrived: a machine that keeps attesting keeps the time
	// of the map request that first proved it.
	AttestedAt time.Time `json:"attestedAt,omitzero"`

	// KeyChangedAt is when a valid signature last arrived under a key
	// other than the stored one, zero while the key never changed. A
	// cleared TPM or a reinstall legitimately produces a new key, so the
	// new one replaces the old, but the change is recorded and audited
	// because it also looks like a machine being impersonated.
	KeyChangedAt time.Time `json:"keyChangedAt,omitzero"`
}
