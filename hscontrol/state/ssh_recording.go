package state

import (
	"fmt"
	"net/netip"
	"slices"
	"time"

	policyv2 "github.com/aislopware/slopscale/hscontrol/policy/v2"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"tailscale.com/tailcfg"
)

// sshRecording is the tailnet's default session recording as the policy
// engine takes it: the recorders from the settings, plus the embedded
// recorder's tag when the server runs one.
func (s *State) sshRecording() (policyv2.SSHRecording, error) {
	settings := s.Settings()

	names := slices.Clone(settings.SSHRecorders)
	if s.cfg.SSHRecording.Enabled && !slices.Contains(names, types.SSHRecorderTag) {
		names = append(names, types.SSHRecorderTag)
	}

	aliases, err := policyv2.ParseSSHRecorders(names)
	if err != nil {
		return policyv2.SSHRecording{}, fmt.Errorf("parsing SSH recorders: %w", err)
	}

	return policyv2.SSHRecording{Recorders: aliases, Enforce: settings.SSHRecordingEnforce}, nil
}

// applySSHRecording hands the current default to the policy engine.
func (s *State) applySSHRecording() error {
	recording, err := s.sshRecording()
	if err != nil {
		return err
	}

	_, err = s.polMan.SetSSHRecording(recording)
	if err != nil {
		return fmt.Errorf("applying SSH recording to policy: %w", err)
	}

	return nil
}

// SetSSHRecording replaces the tailnet's default session recorders and
// whether they are enforced. Every node's SSH policy changes, so the
// change is a policy change.
func (s *State) SetSSHRecording(recorders []string, enforce bool) (change.Change, error) {
	_, err := policyv2.ParseSSHRecorders(recorders)
	if err != nil {
		return change.Change{}, fmt.Errorf("%w: %w", types.ErrSSHRecorderInvalid, err)
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	err = s.db.SaveSSHRecording(recorders, enforce)
	if err != nil {
		return change.Change{}, err
	}

	settings := s.Settings()
	settings.SSHRecorders = slices.Clone(recorders)
	settings.SSHRecordingEnforce = enforce
	s.settings.Store(&settings)

	err = s.applySSHRecording()
	if err != nil {
		return change.Change{}, err
	}

	// The recorders' grant changes who sees whom, and the peer maps are
	// built from the filter.
	s.nodeStore.RebuildPeerMaps()

	return change.PolicyChange(), nil
}

// SSHRecordingFor returns the recorders and failure action a check-mode
// session between src and dst records with; see
// [policyv2.PolicyManager.SSHRecordingFor].
func (s *State) SSHRecordingFor(
	srcNodeID, dstNodeID types.NodeID,
) ([]netip.AddrPort, *tailcfg.SSHRecorderFailureAction) {
	return s.polMan.SSHRecordingFor(s.cfg.ServerURL, srcNodeID, dstNodeID)
}

// NodeByIP finds the node that holds a tailnet address; the recorder
// uses it to name the node a session ran on.
func (s *State) NodeByIP(addr netip.Addr) (types.NodeView, bool) {
	for _, node := range s.nodeStore.ListNodes().All() {
		if slices.Contains(node.IPs(), addr) {
			return node, true
		}
	}

	return types.NodeView{}, false
}

// The recorder's index lives in the database; these are its rows.

// CreateSSHRecording indexes a session as its upload starts.
func (s *State) CreateSSHRecording(rec types.SSHRecording) (types.SSHRecording, error) {
	return s.db.CreateSSHRecording(rec)
}

// FinishSSHRecording records how an upload ended.
func (s *State) FinishSSHRecording(id types.SSHRecordingID, size int64, complete bool) error {
	return s.db.FinishSSHRecording(id, size, complete)
}

// GetSSHRecording reads one recording.
func (s *State) GetSSHRecording(id types.SSHRecordingID) (types.SSHRecording, error) {
	return s.db.GetSSHRecording(id)
}

// ListSSHRecordings pages through recordings, newest first.
func (s *State) ListSSHRecordings(before types.SSHRecordingID, limit int) ([]types.SSHRecording, error) {
	return s.db.ListSSHRecordings(before, limit)
}

// ListSSHRecordingsBefore returns the recordings older than the cutoff.
func (s *State) ListSSHRecordingsBefore(cutoff time.Time) ([]types.SSHRecording, error) {
	return s.db.ListSSHRecordingsBefore(cutoff)
}

// DeleteSSHRecording removes a recording's row; the recorder removes
// the file.
func (s *State) DeleteSSHRecording(id types.SSHRecordingID) error {
	return s.db.DeleteSSHRecording(id)
}
