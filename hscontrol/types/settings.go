package types

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
)

// Settings is the tailnet-wide switches with their current values. The
// zero value is every switch off, which is also what a database without a
// settings row means.
type Settings struct {
	DevicesApprovalOn bool
	UsersApprovalOn   bool
}
