package apiv1

import (
	"context"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerNetworks, registerNetworkSwitch)
}

const tagNetworks = "Networks"

// NetworkRouter is a routing node of a network and whether it advertises
// what the network needs.
type NetworkRouter struct {
	NodeID string `format:"uint64" json:"nodeId"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	// MissingPrefixes are the network's prefixes the node does not
	// advertise; the client needs --advertise-routes for them.
	MissingPrefixes []string `json:"missingPrefixes" nullable:"false"`
	// PrimaryPrefixes are the prefixes the node currently serves.
	PrimaryPrefixes []string `json:"primaryPrefixes" nullable:"false"`
}

// Network is a set of prefixes reached through routing nodes and handed
// out to the members of its groups.
type Network struct {
	ID            string          `format:"uint64"                                       json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Enabled       bool            `json:"enabled"`
	ExitNode      bool            `doc:"The prefixes are the exit routes."               json:"exitNode"`
	Protocol      string          `doc:"One of all, tcp, udp, icmp."                     json:"protocol"`
	Ports         string          `doc:"Ports or ranges for tcp and udp; empty is all."  json:"ports"`
	Prefixes      []string        `json:"prefixes"                                       nullable:"false"`
	RouterNodeIDs []string        `json:"routerNodeIds"                                  nullable:"false"`
	GroupIDs      []string        `doc:"Groups whose machines get the routes."           json:"groupIds"  nullable:"false"` //nolint:lll // struct tag
	Routers       []NetworkRouter `doc:"The routers with what they advertise and serve." json:"routers"   nullable:"false"` //nolint:lll // struct tag
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// NetworkRequestBody creates or replaces a network.
type NetworkRequestBody struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     *bool  `doc:"Defaults to true."      json:"enabled,omitempty"`
	// Protocol and Ports narrow what the groups may reach behind the
	// routers; omitted means every protocol and port.
	Protocol string `doc:"One of all, tcp, udp, icmp; defaults to all."            json:"protocol,omitempty"`
	Ports    string `doc:"Ports or ranges such as 22,80-90, for tcp and udp only." json:"ports,omitempty"`
	// Prefixes are CIDRs or addresses; 0.0.0.0/0 or ::/0 make an exit
	// node offer.
	Prefixes []string `doc:"CIDRs or addresses." json:"prefixes"`
	// RouterNodeIDs are the nodes that route the prefixes; they must
	// advertise them.
	RouterNodeIDs []string `doc:"Nodes that route the prefixes." json:"routerNodeIds,omitempty"`
	// GroupIDs are the groups whose machines get the routes; the builtin
	// All hands them to everyone.
	GroupIDs []string `doc:"Groups whose machines get the routes." json:"groupIds"`
}

type (
	networkIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	networkBodyInput struct {
		Body NetworkRequestBody
	}
	networkUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body NetworkRequestBody
	}
	networkEnabledInput struct {
		ID   string `format:"uint64" path:"id"`
		Body struct {
			Enabled bool `json:"enabled"`
		}
	}
	networkOutput struct {
		Body struct {
			Network Network `json:"network"`
		}
	}
	listNetworksOutput struct {
		Body struct {
			Networks []Network `json:"networks" nullable:"false"`
		}
	}
)

func parseNetworkID(s string) (types.NetworkID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid network id", err)
	}

	return types.NetworkID(id), nil
}

func networkFromBody(body NetworkRequestBody) (types.Network, error) {
	prefixes, err := types.ParseNetworkPrefixes(body.Prefixes)
	if err != nil {
		return types.Network{}, mapError("parsing prefixes", err)
	}

	routers, err := parseNodeIDs(body.RouterNodeIDs)
	if err != nil {
		return types.Network{}, err
	}

	groups, err := parseGroupIDs("groupIds", body.GroupIDs)
	if err != nil {
		return types.Network{}, err
	}

	return types.Network{
		Name:          body.Name,
		Description:   body.Description,
		Enabled:       body.Enabled == nil || *body.Enabled,
		Protocol:      types.AccessProtocol(strings.ToLower(body.Protocol)),
		Ports:         strings.TrimSpace(body.Ports),
		Prefixes:      prefixes,
		RouterNodeIDs: routers,
		GroupIDs:      groups,
	}, nil
}

func prefixStrings(prefixes []netip.Prefix) []string {
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, p.String())
	}

	return out
}

// networkFrom renders the network with the state of its routers.
func networkFrom(b Backend, n types.Network) Network {
	out := Network{
		ID:            formatID(uint64(n.ID)),
		Name:          n.Name,
		Description:   n.Description,
		Enabled:       n.Enabled,
		ExitNode:      n.IsExitNode(),
		Protocol:      string(n.ProtocolOrAll()),
		Ports:         n.Ports,
		Prefixes:      prefixStrings(n.Prefixes),
		RouterNodeIDs: make([]string, 0, len(n.RouterNodeIDs)),
		GroupIDs:      make([]string, 0, len(n.GroupIDs)),
		Routers:       make([]NetworkRouter, 0, len(n.RouterNodeIDs)),
		CreatedAt:     n.CreatedAt,
		UpdatedAt:     n.UpdatedAt,
	}

	for _, id := range n.GroupIDs {
		out.GroupIDs = append(out.GroupIDs, formatID(uint64(id)))
	}

	for _, id := range n.RouterNodeIDs {
		out.RouterNodeIDs = append(out.RouterNodeIDs, formatID(id.Uint64()))
		out.Routers = append(out.Routers, networkRouterFrom(b, n, id))
	}

	return out
}

func networkRouterFrom(b Backend, n types.Network, id types.NodeID) NetworkRouter {
	router := NetworkRouter{
		NodeID:          formatID(id.Uint64()),
		MissingPrefixes: []string{},
		PrimaryPrefixes: []string{},
	}

	node, ok := b.State.GetNodeByID(id)
	if !ok {
		return router
	}

	router.Name = node.GivenName()
	router.Online = node.IsOnline().Valid() && node.IsOnline().Get()
	announced := node.AnnouncedRoutes()

	for _, p := range n.Prefixes {
		if !slices.Contains(announced, p) {
			router.MissingPrefixes = append(router.MissingPrefixes, p.String())
		}
	}

	served := slices.Concat(b.State.GetNodePrimaryRoutes(id), node.ExitRoutes())
	for _, p := range served {
		if n.Covers(p) {
			router.PrimaryPrefixes = append(router.PrimaryPrefixes, p.String())
		}
	}

	return router
}

func auditNetworkDetails(ctx context.Context, n types.Network) {
	audit.Detail(ctx, "enabled", n.Enabled)
	audit.Detail(ctx, "prefixes", prefixStrings(n.Prefixes))
	audit.Detail(ctx, "protocol", string(n.ProtocolOrAll()))
	audit.Detail(ctx, "ports", n.Ports)
	audit.Detail(ctx, "routerNodeIds", n.RouterNodeIDs)
	audit.Detail(ctx, "groupIds", n.GroupIDs)
}

func registerNetworks(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listNetworks",
		Method:      http.MethodGet,
		Path:        "/api/v1/network",
		Summary:     "List networks",
		Description: "Networks are prefixes reached through routing nodes and handed out to the " +
			"machines in their groups, the way NetBird's networks work.",
		Tags:     []string{tagNetworks},
		Security: bearerAuth,
	}, scope.DevicesRoutesRead), func(_ context.Context, _ *struct{}) (*listNetworksOutput, error) {
		model := b.State.AccessModel()

		out := &listNetworksOutput{}
		out.Body.Networks = make([]Network, 0, len(model.Networks))

		for _, n := range model.Networks {
			out.Body.Networks = append(out.Body.Networks, networkFrom(b, n))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNetwork",
		Method:      http.MethodGet,
		Path:        "/api/v1/network/{id}",
		Summary:     "Get network",
		Tags:        []string{tagNetworks},
		Security:    bearerAuth,
	}, scope.DevicesRoutesRead), func(_ context.Context, in *networkIDInput) (*networkOutput, error) {
		id, err := parseNetworkID(in.ID)
		if err != nil {
			return nil, err
		}

		network, err := b.State.GetNetwork(id)
		if err != nil {
			return nil, mapError("getting network", err)
		}

		out := &networkOutput{}
		out.Body.Network = networkFrom(b, network)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createNetwork",
		Method:      http.MethodPost,
		Path:        "/api/v1/network",
		Summary:     "Create network",
		Description: "Approves the prefixes on the routers and hands the routes to the machines " +
			"in the groups. Once the tailnet enforces access, the groups may also reach the prefixes.",
		Tags:     []string{tagNetworks},
		Security: bearerAuth,
	}, scope.DevicesRoutes), "network.create", "network", ""), func(
		ctx context.Context, in *networkBodyInput,
	) (*networkOutput, error) {
		network, err := networkFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		created, c, err := b.State.CreateNetwork(network)
		if err != nil {
			return nil, mapError("creating network", err)
		}

		audit.Target(ctx, "", formatID(uint64(created.ID)), created.Name)
		auditNetworkDetails(ctx, created)

		b.Change(c)

		out := &networkOutput{}
		out.Body.Network = networkFrom(b, created)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateNetwork",
		Method:      http.MethodPut,
		Path:        "/api/v1/network/{id}",
		Summary:     "Replace network",
		Tags:        []string{tagNetworks},
		Security:    bearerAuth,
	}, scope.DevicesRoutes), "network.update", "network", "id"), func(
		ctx context.Context, in *networkUpdateInput,
	) (*networkOutput, error) {
		id, err := parseNetworkID(in.ID)
		if err != nil {
			return nil, err
		}

		network, err := networkFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		network.ID = id

		updated, c, err := b.State.UpdateNetwork(network)
		if err != nil {
			return nil, mapError("updating network", err)
		}

		audit.Target(ctx, "", "", updated.Name)
		auditNetworkDetails(ctx, updated)

		b.Change(c)

		out := &networkOutput{}
		out.Body.Network = networkFrom(b, updated)

		return out, nil
	})
}

// registerNetworkSwitch adds the endpoints that act on a network without
// a body of its own: the enable switch and delete.
func registerNetworkSwitch(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setNetworkEnabled",
		Method:      http.MethodPatch,
		Path:        "/api/v1/network/{id}",
		Summary:     "Enable or disable network",
		Description: "Off withdraws the route approvals the network made and stops handing out its routes.",
		Tags:        []string{tagNetworks},
		Security:    bearerAuth,
	}, scope.DevicesRoutes), "network.update", "network", "id"), func(
		ctx context.Context, in *networkEnabledInput,
	) (*networkOutput, error) {
		id, err := parseNetworkID(in.ID)
		if err != nil {
			return nil, err
		}

		updated, c, err := b.State.SetNetworkEnabled(id, in.Body.Enabled)
		if err != nil {
			return nil, mapError("switching network", err)
		}

		audit.Target(ctx, "", "", updated.Name)
		audit.Detail(ctx, "enabled", updated.Enabled)

		b.Change(c)

		out := &networkOutput{}
		out.Body.Network = networkFrom(b, updated)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteNetwork",
		Method:      http.MethodDelete,
		Path:        "/api/v1/network/{id}",
		Summary:     "Delete network",
		Description: "Withdraws the route approvals the network made.",
		Tags:        []string{tagNetworks},
		Security:    bearerAuth,
	}, scope.DevicesRoutes), "network.delete", "network", "id"), func(
		ctx context.Context, in *networkIDInput,
	) (*emptyOutput, error) {
		id, err := parseNetworkID(in.ID)
		if err != nil {
			return nil, err
		}

		network, err := b.State.GetNetwork(id)
		if err != nil {
			return nil, mapError("deleting network", err)
		}

		audit.Target(ctx, "", "", network.Name)

		c, err := b.State.DeleteNetwork(id)
		if err != nil {
			return nil, mapError("deleting network", err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	})
}
