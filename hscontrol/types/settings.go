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
	// KeyExpiry is the tailnet's key expiry cap; zero means off.
	KeyExpiry time.Duration
}
