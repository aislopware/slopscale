package apiv1

import (
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// The v1 contract follows protojson: 64-bit integers are JSON strings (avoiding
// precision loss above 2^53), timestamps are RFC 3339, and zero values are
// emitted. Hence response fields carry NO omitempty; request types keep
// omitempty so their fields stay optional in the spec.

// formatID renders a uint64 identifier as the contract's decimal string.
func formatID[T ~uint64 | ~uint](id T) string {
	return strconv.FormatUint(uint64(id), 10)
}

// User mirrors the v1 User message.
type User struct {
	ID            string    `format:"uint64"                                                json:"id"`
	Name          string    `json:"name"`
	CreatedAt     time.Time `json:"createdAt"`
	DisplayName   string    `json:"displayName"`
	Email         string    `json:"email"`
	ProviderID    string    `json:"providerId"`
	Provider      string    `json:"provider"`
	ProfilePicURL string    `json:"profilePicUrl"`
	Role          string    `doc:"owner, admin, network-admin, it-admin, auditor or member" json:"role"`

	Approved   bool       `doc:"false while the user waits for an administrator." json:"approved"`
	ApprovedAt *time.Time `json:"approvedAt"                                      nullable:"true"`
}

// userFromView converts a domain user into the v1 response shape, reading
// through the [types.UserView] accessors: Name falls back to Username()
// (email/provider/id) when the stored Name is empty, so OIDC users display
// their email.
func userFromView(u types.UserView) User {
	name := u.Name()
	if name == "" {
		name = u.Username()
	}

	out := User{
		ID:            formatID(u.ID()),
		Name:          name,
		CreatedAt:     u.CreatedAt(),
		DisplayName:   u.DisplayName(),
		Email:         u.Email(),
		ProviderID:    u.ProviderIdentifier().String,
		Provider:      u.Provider(),
		ProfilePicURL: u.ProfilePicURL(),
		Role:          u.Role().String(),
		Approved:      u.ApprovedAt().Valid(),
	}

	if u.ApprovedAt().Valid() {
		at := u.ApprovedAt().Get()
		out.ApprovedAt = &at
	}

	return out
}

// MachineOwner is whose a machine is, sent beside every machine name a
// response carries: a given name like "localhost" says nothing on its own.
type MachineOwner struct {
	Tags          []string `doc:"A tagged machine belongs to its tags, not to a user." json:"tags"   nullable:"false"`
	UserID        string   `doc:"The owning user; empty on a tagged machine."          json:"userId"`
	UserName      string   `json:"userName"`
	DisplayName   string   `json:"displayName"`
	ProfilePicURL string   `json:"profilePicUrl"`
}

// machineOwnerFrom reads the owner through [types.NodeView.IsTagged],
// because a tagged node may still carry the user who created it.
func machineOwnerFrom(node types.NodeView) *MachineOwner {
	out := &MachineOwner{Tags: nonNilStrings(node.Tags().AsSlice())}
	if node.IsTagged() || !node.User().Valid() {
		return out
	}

	user := userFromView(node.User())
	out.UserID, out.UserName = user.ID, user.Name
	out.DisplayName, out.ProfilePicURL = user.DisplayName, user.ProfilePicURL

	return out
}

// machineOwnerByID is [machineOwnerFrom] for a node known only by its ID;
// nil once the node is gone.
func (b Backend) machineOwnerByID(id types.NodeID) *MachineOwner {
	node, ok := b.State.GetNodeByID(id)
	if !ok {
		return nil
	}

	return machineOwnerFrom(node)
}
