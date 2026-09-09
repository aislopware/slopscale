package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"tailscale.com/health"
	"tailscale.com/ipn"
	"tailscale.com/tailcfg"
)

// Refusals an operator can act on: the client would not update itself,
// the diagnostic asked for does not exist, and the machine has not handed
// its preferences over to the tailnet admin.
var (
	ErrClientUpdateRefused = errors.New("the client refused to update")
	ErrUnknownDiagnostic   = errors.New("unknown diagnostic")
	ErrRemoteConfigOff     = errors.New(
		"the machine has not opted into remote configuration; " +
			"run `tailscale set --remote-config` on it first",
	)
)

// c2nMaxDiagnostic bounds a diagnostic dump; the noise handler that
// receives it bounds the whole answer the same way.
const c2nMaxDiagnostic = 4 << 20

// remoteConfigPrefsPath proxies to the client's LocalAPI preferences
// through the remote config c2n endpoint; it works only on a machine
// that opted in.
const remoteConfigPrefsPath = "/remoteapi/localapi/v0/prefs"

// diagnosticCalls is the c2n request behind each diagnostic kind.
var diagnosticCalls = map[types.DiagnosticKind]c2nCall{
	types.DiagnosticPrefs:      {method: http.MethodGet, path: "/debug/prefs"},
	types.DiagnosticNetmap:     {method: http.MethodGet, path: "/debug/netmap"},
	types.DiagnosticMetrics:    {method: http.MethodGet, path: "/debug/metrics"},
	types.DiagnosticGoroutines: {method: http.MethodGet, path: "/debug/goroutines"},
	types.DiagnosticSockstats:  {method: http.MethodPost, path: "/sockstats"},
	types.DiagnosticTKALog:     {method: http.MethodGet, path: "/debug/tka/log"},
}

// c2nJSON asks a connected node and decodes its JSON answer into out. A
// client that answers anything but 200 has refused, and its own message
// is the only explanation the operator gets.
func (s *State) c2nJSON(
	ctx context.Context, nodeID types.NodeID, connected bool, call c2nCall,
	dispatch func(...change.Change), out any,
) error {
	if !connected {
		return ErrNodeNotConnected
	}

	resp, err := s.c2nRoundTrip(ctx, nodeID, call, dispatch)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c2nFailure(resp)
	}

	err = readJSON(resp.Body, out)
	if err != nil {
		return fmt.Errorf("decoding %s: %w", call.path, err)
	}

	return nil
}

// ClientUpdateStatus asks a connected node whether it would update itself
// and whether an update is already running.
func (s *State) ClientUpdateStatus(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) (types.ClientUpdate, error) {
	var resp tailcfg.C2NUpdateResponse

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodGet, path: "/update"}, dispatch, &resp)
	if err != nil {
		return types.ClientUpdate{}, err
	}

	return clientUpdateFrom(resp), nil
}

// StartClientUpdate asks a connected node to update itself now. The client
// refuses unless its owner opted in and the platform supports it, and
// while it is serving SSH sessions unless force says to interrupt them.
func (s *State) StartClientUpdate(
	ctx context.Context, nodeID types.NodeID, connected, force bool, dispatch func(...change.Change),
) (types.ClientUpdate, error) {
	path := "/update"
	if force {
		path += "?force=true"
	}

	var resp tailcfg.C2NUpdateResponse

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodPost, path: path}, dispatch, &resp)
	if err != nil {
		return types.ClientUpdate{}, err
	}

	update := clientUpdateFrom(resp)
	if update.Error != "" {
		return update, fmt.Errorf("%w: %s", ErrClientUpdateRefused, update.Error)
	}

	return update, nil
}

func clientUpdateFrom(resp tailcfg.C2NUpdateResponse) types.ClientUpdate {
	return types.ClientUpdate{
		Enabled:   resp.Enabled,
		Supported: resp.Supported,
		Started:   resp.Started,
		Error:     resp.Err,
	}
}

// ClientHealth asks a connected node for the warnings it would show its
// own user, which is where a node that is connected but not working says
// why. The warnings come back ordered by code.
func (s *State) ClientHealth(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) (types.ClientHealth, error) {
	var state health.State

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodGet, path: "/debug/health"}, dispatch, &state)
	if err != nil {
		return types.ClientHealth{}, err
	}

	out := types.ClientHealth{Warnings: make([]types.ClientWarning, 0, len(state.Warnings))}

	for code, warning := range state.Warnings {
		out.Warnings = append(out.Warnings, types.ClientWarning{
			Code:                string(code),
			Severity:            string(warning.Severity),
			Title:               warning.Title,
			Text:                warning.Text,
			BrokenSince:         warning.BrokenSince,
			ImpactsConnectivity: warning.ImpactsConnectivity,
		})
	}

	slices.SortFunc(out.Warnings, func(a, b types.ClientWarning) int {
		return strings.Compare(a.Code, b.Code)
	})

	return out, nil
}

// ClientDiagnostic fetches one of the dumps a client hands over for
// support and returns it as the client wrote it, with the content type it
// gave, so the operator gets the file rather than a reinterpretation.
func (s *State) ClientDiagnostic(
	ctx context.Context, nodeID types.NodeID, connected bool,
	kind types.DiagnosticKind, dispatch func(...change.Change),
) (string, []byte, error) {
	call, ok := diagnosticCalls[kind]
	if !ok {
		return "", nil, fmt.Errorf("%w: %s", ErrUnknownDiagnostic, kind)
	}

	if !connected {
		return "", nil, ErrNodeNotConnected
	}

	resp, err := s.c2nRoundTrip(ctx, nodeID, call, dispatch)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, c2nFailure(resp)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c2nMaxDiagnostic))
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", call.path, err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return contentType, body, nil
}

// SSHUsernames asks a connected node which logins it would suggest for a
// Tailscale SSH session. The list is a hint, not an authorisation: the
// SSH policy still decides.
func (s *State) SSHUsernames(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) ([]string, error) {
	var resp tailcfg.C2NSSHUsernamesResponse

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodGet, path: "/ssh/usernames"}, dispatch, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Usernames, nil
}

// AppConnectorRoutes asks a connected app connector which addresses it has
// resolved for the domains it answers for, so an operator can see what a
// connector learned without reading its logs.
func (s *State) AppConnectorRoutes(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) (types.AppConnectorRoutes, error) {
	var resp tailcfg.C2NAppConnectorDomainRoutesResponse

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodGet, path: "/appconnector/routes"}, dispatch, &resp)
	if err != nil {
		return types.AppConnectorRoutes{}, err
	}

	return types.AppConnectorRoutes{Domains: resp.Domains}, nil
}

// TLSCertStatus asks a connected node about the certificate it caches for
// its own name, which Serve and Funnel need and which fails quietly.
func (s *State) TLSCertStatus(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) (types.TLSCertStatus, error) {
	var resp tailcfg.C2NTLSCertInfo

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodGet, path: "/tls-cert-status"}, dispatch, &resp)
	if err != nil {
		return types.TLSCertStatus{}, err
	}

	return types.TLSCertStatus{
		Valid:   resp.Valid,
		Missing: resp.Missing,
		Expired: resp.Expired,
		Error:   resp.Error,
	}, nil
}

// ClientPreferences reads the curated preferences off a connected node.
// It goes through the debug endpoint, which every client built with debug
// answers whether or not it opted into remote configuration.
func (s *State) ClientPreferences(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) (types.NodePreferences, error) {
	var prefs ipn.Prefs

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodGet, path: "/debug/prefs"}, dispatch, &prefs)
	if err != nil {
		return types.NodePreferences{}, err
	}

	return types.NodePreferencesFrom(prefs), nil
}

// EditClientPreferences changes the preferences the patch names on a
// connected node and returns what the client ended up with. It needs the
// machine to have opted into remote configuration, which hands the tailnet
// admin its whole LocalAPI, and a client new enough to proxy it.
//
// The current preferences are read back through the same endpoint rather
// than the debug one, because a client without debug still serves this,
// and because the advertised routes are recomputed against them.
func (s *State) EditClientPreferences(
	ctx context.Context, nodeID types.NodeID, connected bool,
	patch types.NodePreferencesPatch, dispatch func(...change.Change),
) (types.NodePreferences, error) {
	node, ok := s.nodeStore.GetNode(nodeID)
	if !ok {
		return types.NodePreferences{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	if !node.Hostinfo().Valid() || !node.Hostinfo().RemoteConfig() || node.CapVer() < types.RemoteConfigCapVer {
		return types.NodePreferences{}, ErrRemoteConfigOff
	}

	var current ipn.Prefs

	err := s.c2nJSON(ctx, nodeID, connected,
		c2nCall{method: http.MethodGet, path: remoteConfigPrefsPath}, dispatch, &current)
	if err != nil {
		return types.NodePreferences{}, err
	}

	masked, err := patch.MaskedPrefs(current)
	if err != nil {
		return types.NodePreferences{}, err
	}

	body, err := json.Marshal(masked)
	if err != nil {
		return types.NodePreferences{}, fmt.Errorf("encoding preferences: %w", err)
	}

	var updated ipn.Prefs

	err = s.c2nJSON(ctx, nodeID, connected, c2nCall{
		method:      http.MethodPatch,
		path:        remoteConfigPrefsPath,
		body:        body,
		contentType: "application/json",
	}, dispatch, &updated)
	if err != nil {
		return types.NodePreferences{}, err
	}

	return types.NodePreferencesFrom(updated), nil
}
