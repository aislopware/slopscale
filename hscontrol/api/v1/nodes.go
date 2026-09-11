package apiv1

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/api/tagguard"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/clientversion"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/danielgtaylor/huma/v2"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

func init() {
	registrations = append(registrations, registerNodes)
}

// errBackfillNotConfirmed guards BackfillNodeIPs behind explicit confirmed=true.
var errBackfillNotConfirmed = errors.New("not confirmed, aborting")

// registerMethodToV1Enum maps the stored register method onto the
// SCREAMING_SNAKE enum string the v1 contract emits.
var registerMethodToV1Enum = map[string]string{
	util.RegisterMethodAuthKey: "REGISTER_METHOD_AUTH_KEY",
	util.RegisterMethodOIDC:    "REGISTER_METHOD_OIDC",
	util.RegisterMethodCLI:     "REGISTER_METHOD_CLI",
}

// Node mirrors the v1 Node message. The protojson contract emits unpopulated
// fields: scalars and slices always (no omitempty), nested messages and optional
// timestamps as JSON null when unset.
type Node struct {
	ID          string          `format:"uint64"                                           json:"id"`
	MachineKey  string          `json:"machineKey"`
	NodeKey     string          `json:"nodeKey"`
	DiscoKey    string          `json:"discoKey"`
	IPAddresses []string        `json:"ipAddresses"                                        nullable:"false"`
	Name        string          `json:"name"`
	User        *User           `json:"user"`
	LastSeen    *time.Time      `json:"lastSeen"                                           nullable:"true"`
	Expiry      *time.Time      `json:"expiry"                                             nullable:"true"`
	PreAuthKey  *NodePreAuthKey `json:"preAuthKey"`
	CreatedAt   time.Time       `json:"createdAt"`
	Approved    bool            `doc:"false while the node waits for an administrator."    json:"approved"`
	ApprovedAt  *time.Time      `json:"approvedAt"                                         nullable:"true"`
	Suspended   bool            `doc:"true while an administrator has suspended the node." json:"suspended"`
	SuspendedAt *time.Time      `json:"suspendedAt"                                        nullable:"true"`

	//nolint:lll // struct tag enum list cannot be wrapped
	RegisterMethod string `enum:"REGISTER_METHOD_UNSPECIFIED,REGISTER_METHOD_AUTH_KEY,REGISTER_METHOD_CLI,REGISTER_METHOD_OIDC" json:"registerMethod"`

	GivenName       string   `json:"givenName"`
	Online          bool     `json:"online"`
	ApprovedRoutes  []string `json:"approvedRoutes"  nullable:"false"`
	AvailableRoutes []string `json:"availableRoutes" nullable:"false"`
	SubnetRoutes    []string `json:"subnetRoutes"    nullable:"false"`
	Tags            []string `json:"tags"            nullable:"false"`

	// SharedWith lists the ids of the users the node is shared with.
	SharedWith []string `doc:"IDs of the users the node is shared with." json:"sharedWith" nullable:"false"`

	GlobalExitNode bool `doc:"true when every client is told to prefer this exit node." json:"globalExitNode"`

	// AnnouncedServices is what the node reports hosting; ApprovedServices
	// the names an operator or the policy let it host. See /api/v1/services.
	AnnouncedServices []NodeService `doc:"The services in the node's serve configuration, as it last reported them." json:"announcedServices" nullable:"false"` //nolint:lll // struct tag
	ApprovedServices  []string      `doc:"The services the node may host; it hosts the ones it also announces."      json:"approvedServices"  nullable:"false"` //nolint:lll // struct tag

	// FunnelEnabled is what the client reports in its Hostinfo once a
	// Funnel endpoint is on; the console marks such machines.
	FunnelEnabled bool `doc:"true while the client has a Funnel endpoint on, exposing a service to the internet through the ingress." json:"funnelEnabled"` //nolint:lll // struct tag

	// ClientVersion is the Tailscale client version the node reported,
	// without the build suffix; UpdateAvailable says a newer stable
	// release exists, once the server has looked it up.
	ClientVersion   string `doc:"The Tailscale client version the node last reported, such as 1.86.2; empty until it connects."                            json:"clientVersion"`   //nolint:lll // struct tag
	UpdateAvailable bool   `doc:"true when a newer stable Tailscale client exists than the one the node runs; see latestClientVersion on the server info." json:"updateAvailable"` //nolint:lll // struct tag

	// OS and OSVersion are what the client reports about the machine it
	// runs on, as Tailscale spells them (linux, macOS, windows, iOS,
	// android, freebsd, tvOS); the console draws the OS mark from them.
	OS        string `doc:"The operating system the client reported, as Tailscale names it (linux, macOS, windows, iOS, android, freebsd); empty until it connects." json:"os"`        //nolint:lll // struct tag
	OSVersion string `doc:"The operating system version the client reported, such as 15.1 or Ubuntu 24.04; empty until it connects."                                 json:"osVersion"` //nolint:lll // struct tag

	// Ephemeral covers both an ephemeral pre-auth key and a client that asked
	// to be ephemeral when it registered.
	Ephemeral bool `doc:"true when the node is deleted on logout or after the ephemeral timeout." json:"ephemeral"`

	// AppConnector is what the client reports once it runs the app
	// connector service; see /api/v1/apps.
	AppConnector bool `doc:"true while the client runs the app connector service (tailscale set --advertise-connector)." json:"appConnector"` //nolint:lll // struct tag

	// SSHServer is what the client reports once it runs Tailscale SSH,
	// which a browser session (POST /api/v1/ssh-session) needs.
	SSHServer bool `doc:"true while the client runs Tailscale SSH (tailscale set --ssh)." json:"sshServer"`

	// NetInfo is the client's last network report: its home relay
	// region, the round trip to each region and what its NAT looks like.
	NetInfo *NodeNetInfo `doc:"The client's last network report; absent until it connects." json:"netInfo,omitempty"`

	// ClientWarnings are what the client itself reports as broken, taken
	// from the warn-* flags of its last map request.
	//nolint:lll // doc tag
	ClientWarnings []string `doc:"Problems the client reports about itself: ip-forwarding-off for a subnet router whose kernel drops forwarded packets, router-unhealthy for a broken route setup, etc-apt-source-disabled when the Tailscale apt source is commented out. A newer client may report flags not listed here. Empty while the client reports none, while it is offline, and after a restart of the server until it polls again." json:"clientWarnings" nullable:"false"`

	// HardwareAttestation is what the client's TPM-backed key proved on
	// its last map request.
	//nolint:lll // doc tag
	HardwareAttestation *NodeHardwareAttestation `doc:"What the machine's hardware attestation key proved; absent until the client signs a map request with one." json:"hardwareAttestation,omitempty"`

	// TPM is what the client found, whether or not it attests with it.
	TPM *NodeTPM `doc:"The TPM the client found; absent when it reported none." json:"tpm,omitempty"`

	// RemoteConfig is what the client reports once its user hands
	// configuration to the control plane.
	//nolint:lll // doc tag
	RemoteConfig bool `doc:"true while the client delegated remote configuration to the control plane (tailscale set --remote-config)." json:"remoteConfig"`
}

// NodeHardwareAttestation is the state of a machine's hardware
// attestation, as the last map request left it.
type NodeHardwareAttestation struct {
	//nolint:lll // doc tag
	Attested bool `doc:"true when the last map request carried a valid signature by the key; it is the node:hardwareAttested posture attribute." json:"attested"`
	//nolint:lll // doc tag
	AttestedAt *time.Time `doc:"When attestation was last gained; it is not refreshed per request." json:"attestedAt" nullable:"true"`
	//nolint:lll // doc tag
	KeyChangedAt *time.Time `doc:"When a signature last arrived under a new key; null while it never did." json:"keyChangedAt" nullable:"true"`
	Key          string     `doc:"The key that last verified, as hwattestpub:<hex>."                       json:"key"`
}

// NodeTPM is [tailcfg.TPMInfo], what the client reports about the TPM it
// found on the machine.
type NodeTPM struct {
	Manufacturer    string `doc:"The four-letter manufacturer code, such as MSFT." json:"manufacturer"`
	Vendor          string `doc:"The vendor string."                               json:"vendor"`
	Model           int    `doc:"The vendor-defined model."                        json:"model"`
	FirmwareVersion uint64 `doc:"The firmware version."                            json:"firmwareVersion"`
	SpecRevision    int    `doc:"The TPM 2.0 specification revision."              json:"specRevision"`
}

// NodeService is one service a node reports hosting.
type NodeService struct {
	Name   string   `doc:"The service name, svc:<label>."                json:"name"`
	Ports  []string `doc:"The protocol and ports the node serves it on." json:"ports"  nullable:"false"`
	Active bool     `doc:"true when the node advertises the service."    json:"active"`
}

// NodePreAuthKey is the PreAuthKey shape embedded in a Node response. The
// /preauthkey endpoints own the standalone request/response surface.
type NodePreAuthKey struct {
	User       *User      `json:"user"`
	ID         string     `format:"uint64"   json:"id"`
	Key        string     `json:"key"`
	Reusable   bool       `json:"reusable"`
	Ephemeral  bool       `json:"ephemeral"`
	Used       bool       `json:"used"`
	Expiration *time.Time `json:"expiration" nullable:"true"`
	CreatedAt  *time.Time `json:"createdAt"  nullable:"true"`
	ACLTags    []string   `json:"aclTags"    nullable:"false"`

	Preauthorized bool `json:"preauthorized"`
}

// SetTagsRequestBody mirrors v1.SetTagsRequest.
type SetTagsRequestBody struct {
	Tags []string `json:"tags,omitempty"`
}

// SetApprovedRoutesRequestBody mirrors v1.SetApprovedRoutesRequest.
type SetApprovedRoutesRequestBody struct {
	Routes []string `json:"routes,omitempty"`
}

// DebugCreateNodeRequestBody mirrors v1.DebugCreateNodeRequest.
type DebugCreateNodeRequestBody struct {
	User   string   `json:"user,omitempty"`
	Key    string   `json:"key,omitempty"`
	Name   string   `json:"name,omitempty"`
	Routes []string `json:"routes,omitempty"`
}

type (
	getNodeInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
	}
	nodeOutput struct {
		Body struct {
			Node Node `json:"node"`
		}
	}
)

type (
	listNodesInput struct {
		User string `query:"user"`
	}
	listNodesOutput struct {
		Body struct {
			Nodes []Node `json:"nodes" nullable:"false"`
		}
	}
)

type (
	deleteNodeInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
	}
	deleteNodeOutput struct {
		Body struct{}
	}
)

// ExpireNodeRequestBody mirrors v1.ExpireNodeRequest. Both fields are optional;
// an absent or all-zero body expires the node immediately, as gRPC does.
type ExpireNodeRequestBody struct {
	Expiry        *time.Time `json:"expiry,omitempty"`
	DisableExpiry bool       `json:"disableExpiry,omitempty"`
}

type expireNodeInput struct {
	NodeID string                 `format:"uint64"  path:"nodeId"`
	Body   *ExpireNodeRequestBody `required:"false"`
}

type renameNodeInput struct {
	NodeID  string `format:"uint64" path:"nodeId"`
	NewName string `path:"newName"`
}

type resetNodeHardwareAttestationInput struct {
	NodeID string `format:"uint64" path:"nodeId"`
}

type setTagsInput struct {
	NodeID string `format:"uint64" path:"nodeId"`
	Body   SetTagsRequestBody
}

type setApprovedRoutesInput struct {
	NodeID string `format:"uint64" path:"nodeId"`
	Body   SetApprovedRoutesRequestBody
}

type registerNodeInput struct {
	User string `query:"user"`
	Key  string `query:"key"`
}

type backfillNodeIPsInput struct {
	Confirmed bool `query:"confirmed"`
}

type backfillNodeIPsOutput struct {
	Body struct {
		Changes []string `json:"changes" nullable:"false"`
	}
}

type debugCreateNodeInput struct {
	Body DebugCreateNodeRequestBody
}

func registerNodes(api huma.API, b Backend) {
	registerNodeReadOps(api, b)
	registerNodeWriteOps(api, b)
	registerNodeAdminOps(api, b)
}

func registerNodeReadOps(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNode",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}",
		Summary:     "Get node",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCoreRead), func(_ context.Context, in *getNodeInput) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		node, ok := b.State.GetNodeByID(nodeID)
		if !ok {
			return nil, huma.Error404NotFound("node not found")
		}

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)
		out.Body.Node.SubnetRoutes = servedRoutes(b, node)

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listNodes",
		Method:      http.MethodGet,
		Path:        "/api/v1/node",
		Summary:     "List nodes",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCoreRead), func(_ context.Context, in *listNodesInput) (*listNodesOutput, error) {
		nodes := b.State.ListNodes()
		if in.User != "" {
			user, err := b.State.GetUserByName(in.User)
			if err != nil {
				return nil, mapError("listing nodes", err)
			}

			nodes = b.State.ListNodesByUser(types.UserID(user.ID))
		}

		out := &listNodesOutput{}
		out.Body.Nodes = make([]Node, nodes.Len())

		for i, node := range nodes.All() {
			n := b.nodeFromView(node)

			// Tags-as-identity: tagged nodes are presented as the special
			// TaggedDevices user.
			if node.IsTagged() {
				user := userFromView(types.TaggedDevices.View())
				n.User = &user
			}

			n.SubnetRoutes = servedRoutes(b, node)

			out.Body.Nodes[i] = n
		}

		// Match the gRPC handler's ascending-ID ordering.
		slices.SortFunc(out.Body.Nodes, func(a, b Node) int {
			return cmpNodeID(a.ID, b.ID)
		})

		return out, nil
	})
}

func registerNodeWriteOps(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteNode",
		Method:      http.MethodDelete,
		Path:        "/api/v1/node/{nodeId}",
		Summary:     "Delete node",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCore), "node.delete", "node", "nodeId"), func(
		ctx context.Context, in *deleteNodeInput,
	) (*deleteNodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		node, ok := b.State.GetNodeByID(nodeID)
		if !ok {
			return nil, huma.Error404NotFound("node not found")
		}

		audit.Target(ctx, "", "", node.GivenName())

		nodeChange, err := b.State.DeleteNode(node)
		if !nodeChange.IsEmpty() {
			b.Change(nodeChange)
		}

		if err != nil {
			return nil, huma.Error500InternalServerError("deleting node", err)
		}

		return &deleteNodeOutput{}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "expireNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/expire",
		Summary:     "Expire node",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCore), "node.expire", "node", "nodeId"), func(
		ctx context.Context, in *expireNodeInput,
	) (*nodeOutput, error) {
		return handleExpireNode(ctx, b, in)
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "renameNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/rename/{newName}",
		Summary:     "Rename node",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCore), "node.rename", "node", "nodeId"), func(
		ctx context.Context, in *renameNodeInput,
	) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "newName", in.NewName)

		node, nodeChange, err := b.State.RenameNode(nodeID, in.NewName)
		if err != nil {
			return nil, mapError("renaming node", err)
		}

		audit.Target(ctx, "", "", node.GivenName())

		b.Change(nodeChange)

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setTags",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/tags",
		Summary:     "Set tags",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCore), "node.tags.set", "node", "nodeId"), func(
		ctx context.Context, in *setTagsInput,
	) (*nodeOutput, error) {
		return handleSetTags(ctx, b, in)
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "resetNodeHardwareAttestation",
		Method:      http.MethodDelete,
		Path:        "/api/v1/node/{nodeId}/hardware-attestation",
		Summary:     "Reset hardware attestation",
		Description: "Forgets what the machine's hardware attestation key proved, so the next map request " +
			"that carries a valid signature starts the record again. The client is not touched and keeps its key.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCore), "node.attestation.reset", "node", "nodeId"), func(
		ctx context.Context, in *resetNodeHardwareAttestationInput,
	) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		node, nodeChange, err := b.State.ResetHardwareAttestation(nodeID)
		if err != nil {
			return nil, mapError("resetting hardware attestation", err)
		}

		audit.Target(ctx, "", "", node.GivenName())

		b.Change(nodeChange)

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)

		return out, nil
	})
}

// handleExpireNode applies gRPC parity: disableExpiry => nil expiry (never
// expires); explicit expiry honoured; absent/zero body expires now. Both set
// is a 400.
func handleExpireNode(ctx context.Context, b Backend, in *expireNodeInput) (*nodeOutput, error) {
	nodeID, err := parseNodeID(in.NodeID)
	if err != nil {
		return nil, err
	}

	var (
		disableExpiry bool
		customExpiry  *time.Time
	)

	if in.Body != nil {
		disableExpiry = in.Body.DisableExpiry
		customExpiry = in.Body.Expiry
	}

	if disableExpiry && customExpiry != nil {
		return nil, huma.Error400BadRequest("cannot set both disable_expiry and expiry")
	}

	if disableExpiry {
		audit.Detail(ctx, "disableExpiry", true)

		node, nodeChange, expErr := b.State.SetNodeExpiry(nodeID, nil)
		if expErr != nil {
			return nil, mapError("expiring node", expErr)
		}

		audit.Target(ctx, "", "", node.GivenName())

		b.Change(nodeChange)

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)

		return out, nil
	}

	expiry := time.Now()
	if customExpiry != nil {
		expiry = *customExpiry

		audit.Detail(ctx, "expiry", expiry.Format(time.RFC3339))
	}

	node, nodeChange, err := b.State.SetNodeExpiry(nodeID, &expiry)
	if err != nil {
		return nil, mapError("expiring node", err)
	}

	audit.Target(ctx, "", "", node.GivenName())

	b.Change(nodeChange)

	out := &nodeOutput{}
	out.Body.Node = b.nodeFromView(node)

	return out, nil
}

func handleSetTags(ctx context.Context, b Backend, in *setTagsInput) (*nodeOutput, error) {
	nodeID, err := parseNodeID(in.NodeID)
	if err != nil {
		return nil, err
	}

	// Tagged nodes must keep at least one tag, so reject an empty set
	// before touching state, as gRPC does.
	if len(in.Body.Tags) == 0 {
		return nil, huma.Error400BadRequest(
			"cannot remove all tags from a node - tagged nodes must have at least one tag",
		)
	}

	for _, tag := range in.Body.Tags {
		tagErr := validateTag(tag)
		if tagErr != nil {
			return nil, huma.Error400BadRequest("setting tags", tagErr)
		}
	}

	_, found := b.State.GetNodeByID(nodeID)
	if !found {
		return nil, huma.Error404NotFound("node not found")
	}

	// An OAuth token may only assign tags its own tags own; SetNodeTags still
	// enforces that each tag exists in the policy. v2 gates the same way
	// (handleSetDeviceTags).
	err = tagguard.AssignOwned(ctx, b.State, in.Body.Tags)
	if err != nil {
		return nil, err
	}

	audit.Detail(ctx, "tags", in.Body.Tags)

	node, nodeChange, err := b.State.SetNodeTags(nodeID, in.Body.Tags)
	if err != nil {
		return nil, huma.Error400BadRequest("setting tags", err)
	}

	audit.Target(ctx, "", "", node.GivenName())

	b.Change(nodeChange)

	out := &nodeOutput{}
	out.Body.Node = b.nodeFromView(node)

	return out, nil
}

func registerNodeAdminOps(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setApprovedRoutes",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/approve_routes",
		Summary:     "Set approved routes",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesRoutes), "node.routes.set", "node", "nodeId"), func(
		ctx context.Context, in *setApprovedRoutesInput,
	) (*nodeOutput, error) {
		return handleSetApprovedRoutes(ctx, b, in)
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "registerNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/register",
		Summary:     "Register node",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCore), "node.register", "", ""), func(
		ctx context.Context, in *registerNodeInput,
	) (*nodeOutput, error) {
		// Registering a node into a user's account is acting for that user,
		// which an OAuth token may not do: its nodes are tagged. Checked
		// before the input, so a token learns nothing from the answer.
		err := tagguard.ActForUser(ctx, "register a node")
		if err != nil {
			return nil, err
		}

		registrationID, err := types.AuthIDFromString(in.Key)
		if err != nil {
			return nil, huma.Error400BadRequest("registering node", err)
		}

		audit.Detail(ctx, "key", auditRegistrationID(in.Key))
		audit.Detail(ctx, "user", in.User)

		user, err := b.State.GetUserByName(in.User)
		if err != nil {
			return nil, mapError("looking up user", err)
		}

		node, nodeChange, err := b.State.HandleNodeFromAuthPath(
			registrationID,
			types.UserID(user.ID),
			nil,
			util.RegisterMethodCLI,
		)
		if err != nil {
			return nil, mapError("registering node", err)
		}

		routeChange, err := b.State.AutoApproveRoutes(node)
		if err != nil {
			return nil, huma.Error500InternalServerError("auto approving routes", err)
		}

		audit.Target(ctx, "node", node.StringID(), node.GivenName())

		// Empty changes are ignored by the change sink.
		b.Change(nodeChange, routeChange)

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "backfillNodeIPs",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/backfillips",
		Summary:     "Backfill node IPs",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCore), "node.backfill_ips", "", ""), func(
		ctx context.Context, in *backfillNodeIPsInput,
	) (*backfillNodeIPsOutput, error) {
		if !in.Confirmed {
			return nil, huma.Error400BadRequest("backfilling node IPs", errBackfillNotConfirmed)
		}

		changes, err := b.State.BackfillNodeIPs()
		if err != nil {
			return nil, huma.Error500InternalServerError("backfilling node IPs", err)
		}

		audit.Detail(ctx, "changes", len(changes))

		out := &backfillNodeIPsOutput{}
		out.Body.Changes = changes

		if out.Body.Changes == nil {
			out.Body.Changes = []string{}
		}

		return out, nil
	})

	if !b.debugNodeAPIEnabled() {
		return
	}

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "debugCreateNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/debug/node",
		Summary:     "Debug create node",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesCore), "node.debug_create", "", ""), func(
		ctx context.Context, in *debugCreateNodeInput,
	) (*nodeOutput, error) {
		return handleDebugCreateNode(ctx, b, in)
	})
}

// debugNodeAPIEnabled reports whether POST /api/v1/debug/node is served. It
// mints a node from key material the caller hands it, which is a development
// tool and not something a production server should offer, so it is
// registered only when the config asks for it and is absent from the
// document that server serves otherwise. A backend with no config is the
// spec generator, which describes every operation.
func (b Backend) debugNodeAPIEnabled() bool {
	return b.Cfg == nil || b.Cfg.Debug.NodeAPIEnabled
}

// auditRegistrationID is what the audit log keeps of a registration id: it
// is the secret a node registers with, so the log keeps only enough of it to
// match one entry against another.
func auditRegistrationID(id string) string {
	const keep = 8

	if len(id) <= keep {
		return id
	}

	return id[:keep]
}

func handleSetApprovedRoutes(ctx context.Context, b Backend, in *setApprovedRoutesInput) (*nodeOutput, error) {
	nodeID, err := parseNodeID(in.NodeID)
	if err != nil {
		return nil, err
	}

	var newApproved []netip.Prefix

	for _, route := range in.Body.Routes {
		prefix, parseErr := netip.ParsePrefix(route)
		if parseErr != nil {
			return nil, huma.Error400BadRequest("parsing route", parseErr)
		}

		// One exit route implies both families, else the client won't
		// annotate the node as an exit node.
		if prefix == tsaddr.AllIPv4() || prefix == tsaddr.AllIPv6() {
			newApproved = append(newApproved, tsaddr.AllIPv4(), tsaddr.AllIPv6())
		} else {
			newApproved = append(newApproved, prefix)
		}
	}

	slices.SortFunc(newApproved, netip.Prefix.Compare)
	newApproved = slices.Compact(newApproved)

	audit.Detail(ctx, "routes", nonNilStrings(util.PrefixesToString(newApproved)))

	node, nodeChange, err := b.State.SetApprovedRoutes(nodeID, newApproved)
	if err != nil {
		return nil, mapError("setting approved routes", err)
	}

	audit.Target(ctx, "", "", node.GivenName())

	b.Change(nodeChange)

	out := &nodeOutput{}
	out.Body.Node = b.nodeFromView(node)
	out.Body.Node.SubnetRoutes = servedRoutes(b, node)

	return out, nil
}

func handleDebugCreateNode(ctx context.Context, b Backend, in *debugCreateNodeInput) (*nodeOutput, error) {
	audit.Target(ctx, "node", "", in.Body.Name)
	audit.Detail(ctx, "user", in.Body.User)

	user, err := b.State.GetUserByName(in.Body.User)
	if err != nil {
		return nil, mapError("looking up user", err)
	}

	routes, err := util.StringToIPPrefix(in.Body.Routes)
	if err != nil {
		return nil, huma.Error400BadRequest("parsing routes", err)
	}

	registrationID, err := types.AuthIDFromString(in.Body.Key)
	if err != nil {
		return nil, huma.Error400BadRequest("debug creating node", err)
	}

	regData := &types.RegistrationData{
		NodeKey:    key.NewNode().Public(),
		MachineKey: key.NewMachine().Public(),
		Hostname:   in.Body.Name,
		Expiry:     &time.Time{}, // zero time, not nil, to keep proto JSON round-trip semantics
	}

	authRegReq := types.NewRegisterAuthRequest(regData)
	b.State.SetAuthCacheEntry(registrationID, authRegReq)

	// Synthetic echo; the real node is created later via the auth path
	// from the cached registration data.
	echoNode := types.Node{
		NodeKey:    regData.NodeKey,
		MachineKey: regData.MachineKey,
		Hostname:   regData.Hostname,
		User:       user,
		Expiry:     &time.Time{},
		LastSeen:   &time.Time{},
		Hostinfo: &tailcfg.Hostinfo{
			Hostname:    in.Body.Name,
			OS:          "TestOS",
			RoutableIPs: routes,
		},
	}

	out := &nodeOutput{}
	out.Body.Node = b.nodeFromView(echoNode.View())

	return out, nil
}

// servedRoutes is what the node actively serves, exit routes included:
// the one shape every handler that fills SubnetRoutes reports.
func servedRoutes(b Backend, node types.NodeView) []string {
	return util.PrefixesToString(
		append(b.State.GetNodePrimaryRoutes(node.ID()), node.ExitRoutes()...),
	)
}

// nodeFromView builds the Node response from a NodeView, reading through the
// view accessors. SubnetRoutes is left empty; callers that serve routes set it
// explicitly.
func (b Backend) nodeFromView(view types.NodeView) Node {
	n := nodeFromView(view)

	if hi := view.Hostinfo(); hi.Valid() {
		n.ClientVersion = clientversion.Short(hi.IPNVersion())
		n.UpdateAvailable = clientversion.Outdated(hi.IPNVersion(), b.State.LatestClientVersion())
		n.OS = hi.OS()
		n.OSVersion = hi.OSVersion()
		n.AppConnector = hi.AppConnector().EqualBool(true)
		n.SSHServer = hi.SSH_HostKeys().Len() > 0
		n.NetInfo = netInfoFrom(view, b.derpRegions())
		n.RemoteConfig = hi.RemoteConfig()

		if tpm, ok := hi.TPM().GetOk(); ok {
			n.TPM = &NodeTPM{
				Manufacturer:    tpm.Manufacturer,
				Vendor:          tpm.Vendor,
				Model:           tpm.Model,
				FirmwareVersion: tpm.FirmwareVersion,
				SpecRevision:    tpm.SpecRevision,
			}
		}
	}

	return n
}

// nodeFromView is [Backend.nodeFromView] without what needs the state.
func nodeFromView(view types.NodeView) Node {
	n := Node{
		ID:                view.StringID(),
		MachineKey:        view.MachineKey().String(),
		NodeKey:           view.NodeKey().String(),
		DiscoKey:          view.DiscoKey().String(),
		IPAddresses:       nonNilStrings(view.IPsAsString()),
		Name:              view.Hostname(),
		CreatedAt:         view.CreatedAt(),
		RegisterMethod:    registerMethodEnum(view.RegisterMethod()),
		GivenName:         view.GivenName(),
		Online:            view.IsOnline().Valid() && view.IsOnline().Get(),
		ApprovedRoutes:    nonNilStrings(util.PrefixesToString(view.ApprovedRoutes().AsSlice())),
		AvailableRoutes:   nonNilStrings(util.PrefixesToString(view.AnnouncedRoutes())),
		AnnouncedServices: nodeServicesFrom(view),
		ApprovedServices:  nonNilStrings(view.ApprovedServices().AsSlice()),
		SubnetRoutes:      []string{},
		Tags:              nonNilStrings(view.Tags().AsSlice()),
		Approved:          view.IsApproved(),
		SharedWith:        sharedWithIDs(view),
		GlobalExitNode:    view.IsGlobalExitNode(),
		FunnelEnabled:     view.FunnelEnabled(),
		Ephemeral:         view.IsEphemeral(),
	}

	if view.ApprovedAt().Valid() {
		at := view.ApprovedAt().Get()
		n.ApprovedAt = &at
	}

	n.ClientWarnings = nonNilStrings(view.ClientWarnings().AsSlice())

	if view.SuspendedAt().Valid() {
		at := view.SuspendedAt().Get()
		n.Suspended = true
		n.SuspendedAt = &at
	}

	if view.User().Valid() {
		user := userFromView(view.User())
		n.User = &user
	}

	if view.AuthKey().Valid() {
		n.PreAuthKey = nodePreAuthKeyFromView(view.AuthKey())
	}

	// A pointer to the zero time is not a timestamp; the schema says null.
	if ls := view.LastSeen(); ls.Valid() && !ls.Get().IsZero() {
		at := ls.Get()
		n.LastSeen = &at
	}

	if exp := view.Expiry(); exp.Valid() && !exp.Get().IsZero() {
		at := exp.Get()
		n.Expiry = &at
	}

	n.HardwareAttestation = hardwareAttestationFrom(view.HardwareAttestation())

	return n
}

// hardwareAttestationFrom renders a node's attestation record, nil for a
// node that never signed a map request with an attestation key.
func hardwareAttestationFrom(view types.HardwareAttestationView) *NodeHardwareAttestation {
	if !view.Valid() {
		return nil
	}

	out := &NodeHardwareAttestation{
		Attested: view.Attested(),
		Key:      view.Key().String(),
	}

	if at := view.AttestedAt(); !at.IsZero() {
		out.AttestedAt = &at
	}

	if at := view.KeyChangedAt(); !at.IsZero() {
		out.KeyChangedAt = &at
	}

	return out
}

// nodePreAuthKeyFromView builds the embedded NodePreAuthKey, masking the key to
// its prefix (legacy plaintext keys are shown in full).
func nodePreAuthKeyFromView(authKey types.PreAuthKeyView) *NodePreAuthKey {
	pak := &NodePreAuthKey{
		ID:        formatID(authKey.ID()),
		Key:       maskedPreAuthKey(authKey),
		Reusable:  authKey.Reusable(),
		Ephemeral: authKey.Ephemeral(),
		Used:      authKey.Used(),
		ACLTags:   nonNilStrings(authKey.Tags().AsSlice()),

		Preauthorized: authKey.Preauthorized(),
	}

	if authKey.User().Valid() {
		user := userFromView(authKey.User())
		pak.User = &user
	}

	if authKey.Expiration().Valid() {
		exp := authKey.Expiration().Get()
		pak.Expiration = &exp
	}

	if authKey.CreatedAt().Valid() {
		created := authKey.CreatedAt().Get()
		pak.CreatedAt = &created
	}

	return pak
}

// registerMethodEnum maps the stored register method onto the v1 enum string,
// defaulting to REGISTER_METHOD_UNSPECIFIED for unknown values.
func registerMethodEnum(method string) string {
	if enum, ok := registerMethodToV1Enum[method]; ok {
		return enum
	}

	return "REGISTER_METHOD_UNSPECIFIED"
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// cmpNodeID orders two decimal node-ID strings numerically, matching the gRPC
// handler's ascending-ID ordering.
func cmpNodeID(a, b string) int {
	ai, _ := strconv.ParseUint(a, 10, 64)
	bi, _ := strconv.ParseUint(b, 10, 64)

	switch {
	case ai < bi:
		return -1
	case ai > bi:
		return 1
	default:
		return 0
	}
}

// sharedWithIDs renders the node's sharees as decimal user ids.
func sharedWithIDs(view types.NodeView) []string {
	out := make([]string, 0, view.SharedWith().Len())
	for _, uid := range view.SharedWith().All() {
		out = append(out, strconv.FormatUint(uint64(uid), 10))
	}

	return out
}

func parseNodeID(s string) (types.NodeID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest(
			"type mismatch, parameter: node_id, error: " + err.Error(),
		)
	}

	return types.NodeID(id), nil
}
