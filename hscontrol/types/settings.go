package types

import (
	"errors"
	"fmt"
	"time"
)

// SettingKey names a tailnet-wide switch stored in the settings table.
type SettingKey string

const (
	// SettingDevicesApprovalOn requires an administrator to approve every
	// newly registered node before it gets peers, unless the node
	// registered with a preauthorized pre-auth key.
	SettingDevicesApprovalOn SettingKey = "devices_approval_on"
	// SettingUsersApprovalOn requires an administrator to approve every
	// user that OIDC login creates before that user can register nodes.
	SettingUsersApprovalOn SettingKey = "users_approval_on"
	// SettingKeyExpiry caps how long a node key stays valid after a
	// login: the client's own request is shortened to it and a login
	// that asks for nothing gets it. It overrides the config file's
	// node.expiry while set; zero leaves the file in charge.
	SettingKeyExpiry SettingKey = "key_expiry"
	// SettingPostureIdentityOn lets the server ask clients for their
	// device identity (hardware serial numbers) over c2n, the way
	// Tailscale's postureIdentityCollectionOn does. Off, nothing is
	// asked and node:serialNumber is never set.
	SettingPostureIdentityOn SettingKey = "posture_identity_on"
	// SettingSSHRecorders is the JSON list of recorder aliases every SSH
	// rule without its own recorder streams sessions to; see
	// docs/ref/ssh-recording.md.
	SettingSSHRecorders SettingKey = "ssh_recorders"
	// SettingSSHRecordingEnforce rejects a session when none of the
	// default recorders is reachable, instead of letting it go on
	// unrecorded.
	SettingSSHRecordingEnforce SettingKey = "ssh_recording_enforce"
	// SettingDeviceAttributesOn lets a machine set its own custom posture
	// attributes over its control connection, the client's
	// alpha-set-device-attrs local API. Off, the request is refused:
	// whoever is root on a machine could otherwise give it any attribute
	// a policy trusts.
	SettingDeviceAttributesOn SettingKey = "device_attributes_on"
	// SettingIDTokenKey holds the identity token signing key as PKCS#8
	// PEM; it is made on first use and shared by every server on the
	// same database. See [State.IDTokenSigner].
	SettingIDTokenKey SettingKey = "id_token_signing_key"
	// SettingTailnetLock holds the tailnet lock state as JSON
	// ([TailnetLockSettings]); the authority's log is the tka_aums
	// table. See [State.TailnetLock].
	SettingTailnetLock SettingKey = "tailnet_lock"
)

// Key expiry bounds: a cap shorter than an hour would log nodes out
// before they finish registering, and Tailscale allows at most 180 days.
const (
	MinKeyExpiry = time.Hour
	MaxKeyExpiry = 365 * 24 * time.Hour
)

// ErrKeyExpiryOutOfRange is returned for a key expiry outside the bounds.
var ErrKeyExpiryOutOfRange = errors.New("key expiry must be 0 or between 1h and 365d")

// ValidateKeyExpiry checks a key expiry setting; zero switches it off.
func ValidateKeyExpiry(d time.Duration) error {
	if d == 0 || (d >= MinKeyExpiry && d <= MaxKeyExpiry) {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrKeyExpiryOutOfRange, d)
}

// Settings is the tailnet-wide switches with their current values. The
// zero value is every switch off, which is also what a database without a
// settings row means.
type Settings struct {
	DevicesApprovalOn bool
	UsersApprovalOn   bool
	PostureIdentityOn bool
	// KeyExpiry is the tailnet's key expiry cap; zero means off.
	KeyExpiry time.Duration
	// SSHRecorders is the tailnet's default session recorders, as
	// policy aliases (tags, hosts, addresses).
	SSHRecorders []string
	// SSHRecordingEnforce refuses SSH sessions no default recorder can
	// take.
	SSHRecordingEnforce bool
	// DeviceAttributesOn lets machines set their own custom posture
	// attributes; see [SettingDeviceAttributesOn].
	DeviceAttributesOn bool
}
