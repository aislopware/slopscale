package apiv1

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations,
		registerNodeClientUpdate,
		registerNodeHealth,
		registerNodeSSHUsernames,
		registerNodeConnectorAndCert,
		registerNodePreferences,
	)
}

// nodeClientUpdateConcurrency bounds how many nodes a bulk update asks at
// once: each ask holds a c2n round trip open for up to fifteen seconds,
// and a console bar starting a hundred of them should not open a hundred.
const nodeClientUpdateConcurrency = 8

// diagnosticKinds is the enum the diagnostic path parameter accepts.
var diagnosticKinds = strings.Join(diagnosticKindStrings(), ",")

func diagnosticKindStrings() []string {
	out := make([]string, 0, len(types.DiagnosticKinds))
	for _, kind := range types.DiagnosticKinds {
		out = append(out, string(kind))
	}

	return out
}

// NodeClientUpdate is what a client says about updating its own Tailscale
// installation.
type NodeClientUpdate struct {
	Enabled   bool   `doc:"Whether the machine's owner allows control to update it."        json:"enabled"`
	Supported bool   `doc:"Whether the platform can update itself at all."                  json:"supported"`
	Started   bool   `doc:"Whether an update is running."                                   json:"started"`
	Error     string `doc:"The reason the client gave for refusing, empty when it did not." json:"error,omitempty"` //nolint:lll // one tag
}

// NodeClientWarning is one unhealthy warnable on a client.
type NodeClientWarning struct {
	Code                string     `doc:"The warnable's identifier."                     json:"code"`
	Severity            string     `doc:"How bad the client considers it."               json:"severity"`
	Title               string     `json:"title"`
	Text                string     `doc:"What the client would tell its own user."       json:"text"`
	BrokenSince         *time.Time `doc:"When it went wrong."                            json:"brokenSince,omitempty"`
	ImpactsConnectivity bool       `doc:"Whether the client thinks traffic is affected." json:"impactsConnectivity"`
}

// NodeClientHealth is the client's own health report, ordered by code.
type NodeClientHealth struct {
	Warnings []NodeClientWarning `doc:"The warnings the client would show its user." json:"warnings" nullable:"false"`
}

// NodeSSHUsernames are the logins a client suggests for a Tailscale SSH
// session to it. They are a hint for the console's terminal; the SSH
// policy still decides who may log in as whom.
type NodeSSHUsernames struct {
	Usernames []string `json:"usernames" nullable:"false"`
}

// NodeAppConnectorRoutes is what an app connector has learned: the
// addresses it resolved for each domain it answers for.
type NodeAppConnectorRoutes struct {
	Domains map[string][]string `json:"domains"`
}

// NodeTLSCertStatus is the state of the certificate a client caches for
// its own MagicDNS name, which Serve and Funnel need.
type NodeTLSCertStatus struct {
	Valid   bool   `json:"valid"`
	Missing bool   `doc:"The client has never fetched one." json:"missing"`
	Expired bool   `json:"expired"`
	Error   string `json:"error,omitempty"`
}

// NodePreferences is the curated part of a client's preferences.
// AdvertiseRoutes leaves the default routes out; offering to be an exit
// node is advertiseExitNode, as the client's own CLI presents it.
type NodePreferences struct {
	AdvertiseRoutes        []string `json:"advertiseRoutes"                                  nullable:"false"`
	AdvertiseExitNode      bool     `json:"advertiseExitNode"`
	AcceptRoutes           bool     `json:"acceptRoutes"`
	AcceptDNS              bool     `json:"acceptDns"`
	ExitNode               string   `doc:"The exit node in use, by stable id or address."    json:"exitNode"`
	ExitNodeAllowLANAccess bool     `json:"exitNodeAllowLanAccess"`
	RunSSH                 bool     `doc:"Whether the client runs Tailscale SSH."            json:"runSsh"`
	ShieldsUp              bool     `doc:"Whether the client blocks incoming traffic."       json:"shieldsUp"`
	Hostname               string   `json:"hostname"`
	AutoUpdateCheck        bool     `json:"autoUpdateCheck"`
	AutoUpdateApply        bool     `json:"autoUpdateApply"`
	AdvertiseConnector     bool     `doc:"Whether the client offers to be an app connector." json:"advertiseConnector"`
	PostureChecking        bool     `json:"postureChecking"`
}

// UpdateNodePreferencesRequestBody changes the fields it names and leaves
// the rest alone.
type UpdateNodePreferencesRequestBody struct {
	AdvertiseRoutes        *[]string `json:"advertiseRoutes,omitempty"`
	AdvertiseExitNode      *bool     `json:"advertiseExitNode,omitempty"`
	AcceptRoutes           *bool     `json:"acceptRoutes,omitempty"`
	AcceptDNS              *bool     `json:"acceptDns,omitempty"`
	ExitNode               *string   `doc:"A stable node id or address; empty clears it." json:"exitNode,omitempty"`
	ExitNodeAllowLANAccess *bool     `json:"exitNodeAllowLanAccess,omitempty"`
	RunSSH                 *bool     `json:"runSsh,omitempty"`
	ShieldsUp              *bool     `json:"shieldsUp,omitempty"`
	Hostname               *string   `json:"hostname,omitempty"`
	AutoUpdateCheck        *bool     `json:"autoUpdateCheck,omitempty"`
	AutoUpdateApply        *bool     `json:"autoUpdateApply,omitempty"`
	AdvertiseConnector     *bool     `json:"advertiseConnector,omitempty"`
	PostureChecking        *bool     `json:"postureChecking,omitempty"`
}

// StartNodeClientUpdateRequestBody asks one node to update itself.
type StartNodeClientUpdateRequestBody struct {
	Force bool `doc:"Update even while the node is serving SSH sessions." json:"force,omitempty"`
}

// StartNodesClientUpdateRequestBody asks several nodes at once.
type StartNodesClientUpdateRequestBody struct {
	NodeIDs []string `doc:"The nodes to update."                              json:"nodeIds"`
	Force   bool     `doc:"Update even while a node is serving SSH sessions." json:"force,omitempty"`
}

// NodeClientUpdateResult is one node's answer in a bulk update.
type NodeClientUpdateResult struct {
	NodeID  string `format:"uint64"                                      json:"nodeId"`
	Started bool   `json:"started"`
	Error   string `doc:"Why the node did not start, empty when it did." json:"error,omitempty"`
}

// NodesClientUpdate is the bulk update's answer, in the order asked.
type NodesClientUpdate struct {
	Results []NodeClientUpdateResult `json:"results" nullable:"false"`
}

type (
	nodeIDInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
	}
	nodeDiagnosticInput struct {
		NodeID string `format:"uint64"                                          path:"nodeId"`
		Kind   string `enum:"prefs,netmap,metrics,goroutines,sockstats,tka-log" path:"kind"`
	}
	startNodeClientUpdateInput struct {
		NodeID string                            `format:"uint64"  path:"nodeId"`
		Body   *StartNodeClientUpdateRequestBody `required:"false"`
	}
	updateNodePreferencesInput struct {
		NodeID string                           `format:"uint64" path:"nodeId"`
		Body   UpdateNodePreferencesRequestBody `required:"true"`
	}
	startNodesClientUpdateInput struct {
		Body StartNodesClientUpdateRequestBody `required:"true"`
	}
	nodeClientUpdateOutput  struct{ Body NodeClientUpdate }
	nodeClientHealthOutput  struct{ Body NodeClientHealth }
	nodeSSHUsernamesOutput  struct{ Body NodeSSHUsernames }
	nodeConnectorOutput     struct{ Body NodeAppConnectorRoutes }
	nodeTLSCertOutput       struct{ Body NodeTLSCertStatus }
	nodePreferencesOutput   struct{ Body NodePreferences }
	nodesClientUpdateOutput struct{ Body NodesClientUpdate }
)

// mapNodeOpError maps a control-to-node failure to a status the console
// can act on: a node that is not there to ask is a conflict, one that
// never answered a gateway timeout, and one that answered with a refusal
// a bad gateway carrying the client's own words.
func mapNodeOpError(msg string, err error) error {
	switch {
	case errors.Is(err, state.ErrC2NTimeout):
		return huma.Error504GatewayTimeout(err.Error())
	case errors.Is(err, state.ErrC2NFailed), errors.Is(err, state.ErrClientUpdateRefused):
		return huma.Error502BadGateway(err.Error())
	case errors.Is(err, state.ErrNodeNotConnected), errors.Is(err, state.ErrRemoteConfigOff):
		return huma.Error409Conflict(err.Error())
	}

	return mapError(msg, err)
}

// nodeToAsk finds the node an operation targets and reports whether its
// client is connected, as [collectNodePosture] does.
func (b Backend) nodeToAsk(id string) (types.NodeView, bool, error) {
	nodeID, err := parseNodeID(id)
	if err != nil {
		return types.NodeView{}, false, err
	}

	view, ok := b.State.GetNodeByID(nodeID)
	if !ok {
		return types.NodeView{}, false, huma.Error404NotFound("node not found")
	}

	return view, view.IsOnline().Valid() && view.IsOnline().Get(), nil
}

func registerNodeClientUpdate(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNodeClientUpdate",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/client-update",
		Summary:     "Get node client update status",
		Description: "Asks the connected node whether it would update its own Tailscale installation " +
			"and whether an update is already running. See /ref/device-management.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCoreRead), func(ctx context.Context, in *nodeIDInput) (*nodeClientUpdateOutput, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		update, err := b.State.ClientUpdateStatus(ctx, view.ID(), online, b.Change)
		if err != nil {
			return nil, mapNodeOpError("getting client update status", err)
		}

		return &nodeClientUpdateOutput{Body: clientUpdateBody(update)}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "startNodeClientUpdate",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/client-update",
		Summary:     "Update the node's Tailscale client",
		Description: "Asks the connected node to update itself now. The client refuses unless its owner " +
			"opted in with `tailscale set --auto-update` or TS_ALLOW_REMOTE_UPDATE, and while it is " +
			"serving SSH sessions unless force is set.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCore), "node.client_update.start", "node", "nodeId"), func(
		ctx context.Context, in *startNodeClientUpdateInput,
	) (*nodeClientUpdateOutput, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		force := in.Body != nil && in.Body.Force

		audit.Target(ctx, "", "", view.GivenName())
		audit.Detail(ctx, "force", force)

		update, err := b.State.StartClientUpdate(ctx, view.ID(), online, force, b.Change)
		if err != nil {
			return nil, mapNodeOpError("starting client update", err)
		}

		return &nodeClientUpdateOutput{Body: clientUpdateBody(update)}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "startNodesClientUpdate",
		Method:      http.MethodPost,
		Path:        "/api/v1/nodes/client-update",
		Summary:     "Update several nodes' Tailscale clients",
		Description: "Asks each named node to update itself, a few at a time, and answers with one " +
			"result per node in the order asked. A node that is offline or refuses fails on its " +
			"own; the others still start.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCore), "node.client_update.start", "", ""), func(
		ctx context.Context, in *startNodesClientUpdateInput,
	) (*nodesClientUpdateOutput, error) {
		results, err := b.startClientUpdates(ctx, in.Body)
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "nodeIds", in.Body.NodeIDs)
		audit.Detail(ctx, "force", in.Body.Force)

		return &nodesClientUpdateOutput{Body: NodesClientUpdate{Results: results}}, nil
	})
}

// startClientUpdates asks each node in turn, a few at a time, and records
// what each answered rather than failing the whole call on the first one
// that is offline or refuses.
func (b Backend) startClientUpdates(
	ctx context.Context, body StartNodesClientUpdateRequestBody,
) ([]NodeClientUpdateResult, error) {
	ids, err := parseNodeIDs(body.NodeIDs)
	if err != nil {
		return nil, err
	}

	results := make([]NodeClientUpdateResult, len(ids))
	slots := make(chan struct{}, nodeClientUpdateConcurrency)

	var wg sync.WaitGroup

	for i, nodeID := range ids {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			results[i] = NodeClientUpdateResult{NodeID: formatID(nodeID)}

			view, ok := b.State.GetNodeByID(nodeID)
			if !ok {
				results[i].Error = "node not found"

				return
			}

			online := view.IsOnline().Valid() && view.IsOnline().Get()

			update, err := b.State.StartClientUpdate(ctx, nodeID, online, body.Force, b.Change)
			if err != nil {
				results[i].Error = err.Error()

				return
			}

			results[i].Started = update.Started
		})
	}

	wg.Wait()

	return results, nil
}

func clientUpdateBody(update types.ClientUpdate) NodeClientUpdate {
	return NodeClientUpdate{
		Enabled:   update.Enabled,
		Supported: update.Supported,
		Started:   update.Started,
		Error:     update.Error,
	}
}

func registerNodeHealth(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNodeClientHealth",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/health",
		Summary:     "Get node client health",
		Description: "Asks the connected node for the warnings it would show its own user, which is " +
			"where a node that is connected but not working says why.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCoreRead), func(ctx context.Context, in *nodeIDInput) (*nodeClientHealthOutput, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		health, err := b.State.ClientHealth(ctx, view.ID(), online, b.Change)
		if err != nil {
			return nil, mapNodeOpError("getting client health", err)
		}

		out := NodeClientHealth{Warnings: make([]NodeClientWarning, 0, len(health.Warnings))}
		for _, w := range health.Warnings {
			out.Warnings = append(out.Warnings, NodeClientWarning{
				Code:                w.Code,
				Severity:            w.Severity,
				Title:               w.Title,
				Text:                w.Text,
				BrokenSince:         w.BrokenSince,
				ImpactsConnectivity: w.ImpactsConnectivity,
			})
		}

		return &nodeClientHealthOutput{Body: out}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "getNodeDiagnostic",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/diagnostics/{kind}",
		Summary:     "Download a node diagnostic",
		Description: "Asks the connected node for one of the dumps it hands over for support and " +
			"answers with it as the client wrote it: " + diagnosticKinds + ". A client built " +
			"without its debug endpoints refuses.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The diagnostic, as the client wrote it.",
				Content: map[string]*huma.MediaType{
					"application/octet-stream": {Schema: &huma.Schema{Type: attrString, Format: "binary"}},
				},
			},
		},
	}, scope.DevicesCore), "node.diagnostics.read", "node", "nodeId"), func(
		ctx context.Context, in *nodeDiagnosticInput,
	) (*huma.StreamResponse, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		audit.Target(ctx, "", "", view.GivenName())
		audit.Detail(ctx, "kind", in.Kind)

		contentType, body, err := b.State.ClientDiagnostic(
			ctx, view.ID(), online, types.DiagnosticKind(in.Kind), b.Change)
		if err != nil {
			return nil, mapNodeOpError("getting node diagnostic", err)
		}

		filename := view.GivenName() + "-" + in.Kind + "." + diagnosticExtension(contentType)

		return &huma.StreamResponse{Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", contentType)
			ctx.SetHeader("Content-Disposition", `attachment; filename="`+filename+`"`)
			ctx.SetStatus(http.StatusOK)

			_, _ = ctx.BodyWriter().Write(body)
		}}, nil
	})
}

// diagnosticExtension names the file after what the client sent: the
// debug dumps are either JSON or plain text.
func diagnosticExtension(contentType string) string {
	if strings.Contains(contentType, "json") {
		return "json"
	}

	return "txt"
}

func registerNodeSSHUsernames(api huma.API, b Backend) {
	huma.Register(api, huma.Operation{
		OperationID: "getNodeSSHUsernames",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/ssh-usernames",
		Summary:     "Get node SSH username hints",
		Description: "Asks the connected node which logins it would suggest for a Tailscale SSH " +
			"session, so the console's terminal can offer them. The hints are not an " +
			"authorisation; the SSH policy still decides. Visible to whoever may open a session " +
			"to the node: without the devices:core:read scope, a node of the caller's own or one " +
			"their machines can already reach.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, func(ctx context.Context, in *nodeIDInput) (*nodeSSHUsernamesOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		view, ok := b.State.GetNodeByID(nodeID)
		if !ok || !b.canOpenSSHSession(caller(ctx), view) {
			return nil, huma.Error404NotFound("node not found")
		}

		online := view.IsOnline().Valid() && view.IsOnline().Get()

		usernames, err := b.State.SSHUsernames(ctx, nodeID, online, b.Change)
		if err != nil {
			return nil, mapNodeOpError("getting ssh usernames", err)
		}

		return &nodeSSHUsernamesOutput{Body: NodeSSHUsernames{Usernames: nonNilStrings(usernames)}}, nil
	})
}

func registerNodeConnectorAndCert(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNodeAppConnectorRoutes",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/app-connector-routes",
		Summary:     "Get learned app connector routes",
		Description: "Asks the connected node which addresses it has resolved for the domains it " +
			"answers for as an app connector. A node that is not a connector answers with none.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCoreRead), func(ctx context.Context, in *nodeIDInput) (*nodeConnectorOutput, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		routes, err := b.State.AppConnectorRoutes(ctx, view.ID(), online, b.Change)
		if err != nil {
			return nil, mapNodeOpError("getting app connector routes", err)
		}

		domains := make(map[string][]string, len(routes.Domains))

		for domain, addrs := range routes.Domains {
			out := make([]string, 0, len(addrs))
			for _, addr := range addrs {
				out = append(out, addr.String())
			}

			slices.Sort(out)
			domains[domain] = out
		}

		return &nodeConnectorOutput{Body: NodeAppConnectorRoutes{Domains: domains}}, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNodeTLSCertStatus",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/tls-cert",
		Summary:     "Get node TLS certificate status",
		Description: "Asks the connected node about the certificate it caches for its own MagicDNS " +
			"name, which Serve and Funnel need and which fails quietly when it cannot be renewed.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCoreRead), func(ctx context.Context, in *nodeIDInput) (*nodeTLSCertOutput, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		status, err := b.State.TLSCertStatus(ctx, view.ID(), online, b.Change)
		if err != nil {
			return nil, mapNodeOpError("getting tls certificate status", err)
		}

		return &nodeTLSCertOutput{Body: NodeTLSCertStatus{
			Valid:   status.Valid,
			Missing: status.Missing,
			Expired: status.Expired,
			Error:   status.Error,
		}}, nil
	})
}

func registerNodePreferences(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNodePreferences",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/preferences",
		Summary:     "Get node preferences",
		Description: "Asks the connected node for the preferences its owner set: the routes it " +
			"advertises, whether it accepts routes and DNS, which exit node it uses and the rest " +
			"of the curated set. Reading needs no opt-in; changing them does.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCoreRead), func(ctx context.Context, in *nodeIDInput) (*nodePreferencesOutput, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		prefs, err := b.State.ClientPreferences(ctx, view.ID(), online, b.Change)
		if err != nil {
			return nil, mapNodeOpError("getting node preferences", err)
		}

		return &nodePreferencesOutput{Body: preferencesBody(prefs)}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateNodePreferences",
		Method:      http.MethodPatch,
		Path:        "/api/v1/node/{nodeId}/preferences",
		Summary:     "Change node preferences",
		Description: "Changes the preferences the body names on the connected node and answers with " +
			"what the client ended up with. Needs the machine to have opted in by running " +
			"`tailscale set --remote-config` on it, which hands the tailnet admin its whole " +
			"local API; without that the node answers 409.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCore), "node.preferences.update", "node", "nodeId"), func(
		ctx context.Context, in *updateNodePreferencesInput,
	) (*nodePreferencesOutput, error) {
		view, online, err := b.nodeToAsk(in.NodeID)
		if err != nil {
			return nil, err
		}

		patch := preferencesPatch(in.Body)
		if patch.IsEmpty() {
			return nil, huma.Error422UnprocessableEntity("the patch names no preference to change")
		}

		audit.Target(ctx, "", "", view.GivenName())
		audit.Detail(ctx, "preferences", patch.Changed())

		prefs, err := b.State.EditClientPreferences(ctx, view.ID(), online, patch, b.Change)
		if err != nil {
			return nil, mapNodeOpError("changing node preferences", err)
		}

		return &nodePreferencesOutput{Body: preferencesBody(prefs)}, nil
	})
}

func preferencesBody(prefs types.NodePreferences) NodePreferences {
	return NodePreferences{
		AdvertiseRoutes:        nonNilStrings(prefs.AdvertiseRoutes),
		AdvertiseExitNode:      prefs.AdvertiseExitNode,
		AcceptRoutes:           prefs.AcceptRoutes,
		AcceptDNS:              prefs.AcceptDNS,
		ExitNode:               prefs.ExitNode,
		ExitNodeAllowLANAccess: prefs.ExitNodeAllowLANAccess,
		RunSSH:                 prefs.RunSSH,
		ShieldsUp:              prefs.ShieldsUp,
		Hostname:               prefs.Hostname,
		AutoUpdateCheck:        prefs.AutoUpdateCheck,
		AutoUpdateApply:        prefs.AutoUpdateApply,
		AdvertiseConnector:     prefs.AdvertiseConnector,
		PostureChecking:        prefs.PostureChecking,
	}
}

func preferencesPatch(body UpdateNodePreferencesRequestBody) types.NodePreferencesPatch {
	return types.NodePreferencesPatch{
		AdvertiseRoutes:        body.AdvertiseRoutes,
		AdvertiseExitNode:      body.AdvertiseExitNode,
		AcceptRoutes:           body.AcceptRoutes,
		AcceptDNS:              body.AcceptDNS,
		ExitNode:               body.ExitNode,
		ExitNodeAllowLANAccess: body.ExitNodeAllowLANAccess,
		RunSSH:                 body.RunSSH,
		ShieldsUp:              body.ShieldsUp,
		Hostname:               body.Hostname,
		AutoUpdateCheck:        body.AutoUpdateCheck,
		AutoUpdateApply:        body.AutoUpdateApply,
		AdvertiseConnector:     body.AdvertiseConnector,
		PostureChecking:        body.PostureChecking,
	}
}
