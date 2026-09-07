package state

import (
	"errors"
	"fmt"
	"time"

	hsdb "github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
)

var (
	// ErrUserNotApproved is returned when a user who is still waiting for
	// an administrator's approval tries to register a node.
	ErrUserNotApproved = errors.New("user is waiting for approval")
	// ErrUnknownSetting is returned for a setting key the server does not know.
	ErrUnknownSetting = hsdb.ErrUnknownSetting
)

// Settings returns the tailnet-wide switches as last loaded or written.
// A nil State, as API tests without state build, has every switch off.
func (s *State) Settings() types.Settings {
	if s == nil {
		return types.Settings{}
	}

	if p := s.settings.Load(); p != nil {
		return *p
	}

	return types.Settings{}
}

// SetKeyExpiry writes the key expiry cap. It applies to the next login of
// every node and touches nothing that is already registered, so no
// change goes out.
func (s *State) SetKeyExpiry(d time.Duration) error {
	err := types.ValidateKeyExpiry(d)
	if err != nil {
		return err
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	err = s.db.SaveKeyExpiry(d)
	if err != nil {
		return err
	}

	settings := s.Settings()
	settings.KeyExpiry = d
	s.settings.Store(&settings)

	return nil
}

// SetSetting writes one tailnet-wide switch. Switching an approval off
// admits everything that was waiting, so nothing stays stuck behind a
// requirement that no longer exists; the returned change carries that
// fan-out.
func (s *State) SetSetting(key types.SettingKey, on bool) (change.Change, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	settings := s.Settings()

	switch key {
	case types.SettingDevicesApprovalOn:
		settings.DevicesApprovalOn = on
	case types.SettingUsersApprovalOn:
		settings.UsersApprovalOn = on
	case types.SettingPostureIdentityOn:
		settings.PostureIdentityOn = on
	case types.SettingDNS:
		return change.Change{}, fmt.Errorf("%w: %q is not a switch, see SetDNS", ErrUnknownSetting, key)
	case types.SettingKeyExpiry:
		return change.Change{}, fmt.Errorf("%w: %q is not a switch, see SetKeyExpiry", ErrUnknownSetting, key)
	case types.SettingSSHRecorders, types.SettingSSHRecordingEnforce:
		return change.Change{}, fmt.Errorf("%w: %q is not a switch, see SetSSHRecording", ErrUnknownSetting, key)
	default:
		return change.Change{}, fmt.Errorf("%w: %q", ErrUnknownSetting, key)
	}

	err := s.db.SaveSetting(key, on)
	if err != nil {
		return change.Change{}, err
	}

	s.settings.Store(&settings)

	if on {
		return change.Change{}, nil
	}

	switch key {
	case types.SettingDevicesApprovalOn:
		return s.approvePendingNodes()
	case types.SettingUsersApprovalOn:
		return s.approvePendingUsers()
	case types.SettingDNS, types.SettingKeyExpiry, types.SettingPostureIdentityOn,
		types.SettingSSHRecorders, types.SettingSSHRecordingEnforce:
		return change.Change{}, nil
	default:
		return change.Change{}, nil
	}
}

// approvePendingNodes admits every node waiting for device approval.
func (s *State) approvePendingNodes() (change.Change, error) {
	now := time.Now().UTC()

	ids, err := hsdb.Write(s.db, func(tx *hsdb.Tx) ([]types.NodeID, error) {
		return hsdb.ApproveAllNodes(tx, now)
	})
	if err != nil {
		return change.Change{}, err
	}

	if len(ids) == 0 {
		return change.Change{}, nil
	}

	for _, id := range ids {
		s.nodeStore.UpdateNode(id, func(node *types.Node) {
			node.ApprovedAt = &now
		})
	}

	return s.policyChangeAfterApproval()
}

// approvePendingUsers admits every user waiting for users approval.
func (s *State) approvePendingUsers() (change.Change, error) {
	now := time.Now().UTC()

	ids, err := hsdb.Write(s.db, func(tx *hsdb.Tx) ([]types.UserID, error) {
		return hsdb.ApproveAllUsers(tx, now)
	})
	if err != nil {
		return change.Change{}, err
	}

	if len(ids) == 0 {
		return change.Change{}, nil
	}

	return s.updatePolicyManagerUsers()
}

// SetNodeApproval admits a node to the tailnet or withdraws it again. An
// unapproved node stays registered and keeps its address but gets no
// peers and no peer sees it; the change tells every node, and the node
// itself, so the client leaves (or enters) its "needs machine auth" state.
func (s *State) SetNodeApproval(nodeID types.NodeID, approved bool) (types.NodeView, change.Change, error) {
	var approvedAt *time.Time
	if approved {
		approvedAt = new(time.Now().UTC())
	}

	var wasApproved bool

	n, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		wasApproved = node.IsApproved()
		node.ApprovedAt = approvedAt
	})
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	// persistNodeToDB leaves approved_at alone, as it does expiry.
	err := s.db.NodeSetApproval(nodeID, approvedAt)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("setting node approval in database: %w", err)
	}

	if approved && !wasApproved {
		s.emitNodeApproved(n)
	}

	c, err := s.policyChangeAfterApproval()
	if err != nil {
		return n, change.Change{}, err
	}

	return n, c, nil
}

// policyChangeAfterApproval refreshes the policy manager's node list and
// returns the tailnet-wide recompute that approval changes need: every
// node's peer list may have gained or lost the node, and every node gets
// its own self node so the clients of the nodes admitted or withdrawn see
// MachineAuthorized flip. Self is included for all rather than through
// [change.Change.OriginNode] because one change carries a single origin
// while an approval switch can admit many nodes at once.
func (s *State) policyChangeAfterApproval() (change.Change, error) {
	_, err := s.updatePolicyManagerNodes()
	if err != nil {
		return change.Change{}, fmt.Errorf("updating policy manager after approval change: %w", err)
	}

	c := change.PolicyChange()
	c.Reason = "node approval"
	c.IncludeSelf = true

	return c, nil
}

// SetUserApproval admits a user to the tailnet or withdraws them again.
// Withdrawing also withdraws every node the user owns; admitting approves
// those nodes when device approval is off, otherwise they wait their turn.
func (s *State) SetUserApproval(userID types.UserID, approved bool) (*types.User, change.Change, error) {
	var approvedAt *time.Time
	if approved {
		approvedAt = new(time.Now().UTC())
	}

	err := s.db.Write(func(tx *hsdb.Tx) error {
		return hsdb.UserSetApproval(tx, userID, approvedAt)
	})
	if err != nil {
		return nil, change.Change{}, err
	}

	user, err := s.db.GetUserByID(userID)
	if err != nil {
		return nil, change.Change{}, fmt.Errorf("reloading user after approval change: %w", err)
	}

	if approved {
		s.emitUserApproved(user)
	}

	c, err := s.updatePolicyManagerUsers()
	if err != nil {
		return user, change.Change{}, err
	}

	if approved && s.Settings().DevicesApprovalOn {
		return user, c, nil
	}

	for _, node := range s.ListNodesByUser(userID).All() {
		if node.IsApproved() == approved {
			continue
		}

		_, nodeChange, err := s.SetNodeApproval(node.ID(), approved)
		if err != nil {
			return user, change.Change{}, err
		}

		c = c.Merge(nodeChange)
	}

	return user, c, nil
}

// approvedAtRegistration decides whether a node registered right now is
// admitted immediately: yes unless device approval is on and the node did
// not present a preauthorized key.
func (s *State) approvedAtRegistration(pak *types.PreAuthKey) *time.Time {
	if s.Settings().DevicesApprovalOn && (pak == nil || !pak.Preauthorized) {
		return nil
	}

	return new(time.Now().UTC())
}

// requireApprovedUser refuses registration for a user still waiting for
// approval. Tagged nodes carry no owner and are never refused here.
func requireApprovedUser(user *types.User) error {
	if user != nil && (user.ApprovedAt == nil || user.ApprovedAt.IsZero()) {
		return fmt.Errorf("%w: %s", ErrUserNotApproved, user.Username())
	}

	return nil
}

// admittedPeerCandidates drops nodes that are waiting for approval or
// suspended before the policy builds the peer map, so such a node has no
// peers and appears in nobody's.
func admittedPeerCandidates(nodes []types.NodeView) []types.NodeView {
	admitted := nodes[:0:0]

	for _, n := range nodes {
		if n.IsAdmitted() {
			admitted = append(admitted, n)
		}
	}

	return admitted
}

// SetNodeSuspension suspends a node or lifts the suspension. A suspended
// node stays registered and keeps its addresses, but it gets no peers, no
// peer sees it and its client is told it is not authorized until the
// suspension is lifted; the change reaches every node and the node
// itself, as an approval change does.
func (s *State) SetNodeSuspension(nodeID types.NodeID, suspended bool) (types.NodeView, change.Change, error) {
	var suspendedAt *time.Time
	if suspended {
		suspendedAt = new(time.Now().UTC())
	}

	var wasSuspended bool

	n, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		wasSuspended = node.IsSuspended()
		node.SuspendedAt = suspendedAt
	})
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	// persistNodeToDB leaves suspended_at alone, as it does approved_at.
	err := s.db.NodeSetSuspension(nodeID, suspendedAt)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("setting node suspension in database: %w", err)
	}

	if suspended != wasSuspended {
		s.emitNodeSuspension(n, suspended)
	}

	c, err := s.policyChangeAfterApproval()
	if err != nil {
		return n, change.Change{}, err
	}

	c.Reason = "node suspension"

	return n, c, nil
}
