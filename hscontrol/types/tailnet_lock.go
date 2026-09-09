package types

import (
	"errors"
	"time"

	"tailscale.com/types/tkatype"
)

// Tailnet lock on the server side; see docs/ref/tailnet-lock.md. The
// tailnet's nodes hold the signing keys and produce every authority
// update message (AUM); the server keeps the log, checks and stores the
// node key signatures, and tells every node the head so they sync.

var (
	// ErrTailnetLockEnabled is returned for an initialisation while the
	// lock is already on.
	ErrTailnetLockEnabled = errors.New("tailnet lock is already enabled")
	// ErrTailnetLockDisabled is returned for a lock operation while the
	// lock is off.
	ErrTailnetLockDisabled = errors.New("tailnet lock is not enabled")
	// ErrTailnetLockNoPendingInit is returned when init/finish arrives
	// without an init/begin from the same node.
	ErrTailnetLockNoPendingInit = errors.New("no tailnet lock initialisation is pending for this node")
	// ErrTailnetLockMissingSignature is returned when init/finish leaves a
	// node unsigned.
	ErrTailnetLockMissingSignature = errors.New("missing node key signature")
	// ErrTailnetLockBadSignature is returned for a node key signature the
	// authority does not accept.
	ErrTailnetLockBadSignature = errors.New("node key signature does not verify")
	// ErrTailnetLockBadSecret is returned for a disablement secret the
	// authority does not accept.
	ErrTailnetLockBadSecret = errors.New("disablement secret does not verify")
	// ErrTailnetLockNoSupportSecret is returned when the API is asked to
	// disable the lock and no support disablement secret was stored.
	ErrTailnetLockNoSupportSecret = errors.New("no support disablement secret was recorded when the lock was enabled")
	// ErrTailnetLockNotAdmin is returned when a node that is not owned by
	// an administrator tries to enable the lock.
	ErrTailnetLockNotAdmin = errors.New("only a node owned by an owner or admin may enable tailnet lock")
)

// TailnetLockSettings is what the settings table records about tailnet
// lock besides the AUM log: whether it is on, where the log starts and
// the secrets that switch it off.
type TailnetLockSettings struct {
	// Enabled is whether the lock is on.
	Enabled bool `json:"enabled"`
	// Genesis is the hash of the genesis AUM (tka.AUMHash.MarshalText),
	// the AUM a joining node bootstraps from.
	Genesis string `json:"genesis,omitempty"`
	// LastActiveAncestor is the oldest AUM the authority still walks
	// from (tka.Chonk.LastActiveAncestor).
	LastActiveAncestor string `json:"lastActiveAncestor,omitempty"`
	// SupportDisablement is the disablement secret the initialising
	// client minted for the operator (tailscale lock init
	// --gen-disablement-for-support), which the API's disable uses.
	SupportDisablement []byte `json:"supportDisablement,omitempty"`
	// DisablementSecret is the secret the lock was last switched off
	// with; a node that still has the lock on fetches it to switch off.
	DisablementSecret []byte `json:"disablementSecret,omitempty"`
	// EnabledAt and DisabledAt record the last switch.
	EnabledAt  *time.Time `json:"enabledAt,omitempty"`
	DisabledAt *time.Time `json:"disabledAt,omitempty"`
}

// TKAAUM is one authority update message of the tailnet lock log, as
// the tka_aums table stores it.
type TKAAUM struct {
	// Hash is the AUM's hash, tka.AUMHash.MarshalText.
	Hash string
	// PrevHash is the parent's hash; empty for the genesis.
	PrevHash string
	// AUM is the serialised message, tka.AUM.Serialize.
	AUM tkatype.MarshaledAUM
	// CommittedAt is when the server verified and stored it.
	CommittedAt time.Time
}

// TailnetLockKey is one signing key the authority trusts.
type TailnetLockKey struct {
	// ID is the key's identifier, hex, as tailscale lock status shows it.
	ID string `json:"id"`
	// Public is the key in tlpub: form.
	Public string `json:"public"`
	// Votes is the key's weight in the authority.
	Votes uint `json:"votes"`
}

// TailnetLockStatus is what the API and the console show about the lock.
type TailnetLockStatus struct {
	Enabled bool `json:"enabled"`
	// Head is the hash of the latest AUM while the lock is on.
	Head string `json:"head,omitempty"`
	// Keys are the trusted signing keys while the lock is on.
	Keys []TailnetLockKey `json:"keys"`
	// SupportDisablementAvailable is whether the API can switch the
	// lock off, which needs the secret the initialising client minted.
	SupportDisablementAvailable bool `json:"supportDisablementAvailable"`
	// Signed and Unsigned are the nodes with and without a valid node
	// key signature; an unsigned node is locked out while the lock is on.
	Signed   []NodeID `json:"signed"`
	Unsigned []NodeID `json:"unsigned"`

	EnabledAt  *time.Time `json:"enabledAt,omitempty"`
	DisabledAt *time.Time `json:"disabledAt,omitempty"`
}
