package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"tailscale.com/tailcfg"
)

func init() {
	registrations = append(registrations, registerServices, registerServiceApprovals)
}

const tagServices = "Services"

// Service is a tailnet service (Tailscale Services): a name with a pair
// of addresses of its own that approved nodes host. See docs/ref/services.md.
type Service struct {
	ID          string    `format:"uint64"                                      json:"id"`
	Name        string    `doc:"The service's name, svc:<label>."               json:"name"`
	DisplayName string    `doc:"The shown label; empty falls back to the name." json:"displayName"`
	Comment     string    `doc:"The operator's note."                           json:"comment"`
	Ports       []string  `doc:"The ports clients are told about."              json:"ports"       nullable:"false"`
	Addresses   []string  `doc:"The service's own tailnet addresses."           json:"addresses"   nullable:"false"`
	DNSName     string    `doc:"The service's MagicDNS name."                   json:"dnsName"`
	Hosts       []Host    `doc:"The nodes that announce or are approved."       json:"hosts"       nullable:"false"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Host is a node's standing towards a service.
type Host struct {
	NodeID    string   `format:"uint64"                                        json:"nodeId"`
	Name      string   `doc:"The node's given name."                           json:"name"`
	Announced bool     `doc:"true when the node's serve config announces it."  json:"announced"`
	Ports     []string `doc:"The protocol and ports the node serves it on."    json:"ports"     nullable:"false"`
	Active    bool     `doc:"true when the node advertises the service."       json:"active"`
	Approved  bool     `doc:"true when the node may host the service."         json:"approved"`
	Primary   bool     `doc:"true when clients route to this node for it now." json:"primary"`
}

// CreateServiceRequestBody creates a service.
type CreateServiceRequestBody struct {
	Name        string   `doc:"svc:<label> or the label alone; a DNS label." json:"name"            minLength:"1"`
	DisplayName string   `json:"displayName,omitempty"`
	Comment     string   `json:"comment,omitempty"`
	Ports       []string `doc:"tcp:443, udp:53-60 or tcp:*."                 json:"ports,omitempty"`
}

// UpdateServiceRequestBody changes a service; absent fields keep their
// value. The name and the addresses never change.
type UpdateServiceRequestBody struct {
	DisplayName *string   `json:"displayName,omitempty"`
	Comment     *string   `json:"comment,omitempty"`
	Ports       *[]string `json:"ports,omitempty"`
}

// SetApprovedServicesRequestBody replaces the services a node may host.
type SetApprovedServicesRequestBody struct {
	Services []string `doc:"Service names; an empty list withdraws every approval." json:"services" nullable:"false"`
}

type (
	serviceOutput struct {
		Body struct {
			Service Service `json:"service"`
		}
	}
	listServicesOutput struct {
		Body struct {
			Services []Service `json:"services" nullable:"false"`
		}
	}
	createServiceInput struct {
		Body CreateServiceRequestBody
	}
	serviceByNameInput struct {
		Name string `doc:"svc:<label> or the label alone." path:"name"`
	}
	updateServiceInput struct {
		Name string `path:"name"`
		Body UpdateServiceRequestBody
	}
	setApprovedServicesInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
		Body   SetApprovedServicesRequestBody
	}
)

func nodeServicesFrom(view types.NodeView) []NodeService {
	announced := view.AnnouncedServices()
	out := make([]NodeService, 0, len(announced))

	for _, svc := range announced {
		out = append(out, NodeService{
			Name:   string(svc.Name),
			Ports:  nonNilStrings(types.ServicePortsStrings(svc.Ports)),
			Active: svc.Active,
		})
	}

	return out
}

func (b Backend) serviceFrom(svc types.VIPService, hosts map[tailcfg.ServiceName]types.NodeID) Service {
	out := Service{
		ID:          strconv.FormatUint(svc.ID.Uint64(), 10),
		Name:        string(svc.Name),
		DisplayName: svc.DisplayName,
		Comment:     svc.Comment,
		Ports:       nonNilStrings(types.ServicePortsStrings(svc.Ports)),
		Addresses:   []string{},
		Hosts:       []Host{},
		CreatedAt:   svc.CreatedAt,
		UpdatedAt:   svc.UpdatedAt,
	}

	for _, addr := range svc.Addrs() {
		out.Addresses = append(out.Addresses, addr.String())
	}

	if b.Cfg != nil && b.Cfg.BaseDomain != "" {
		out.DNSName = svc.DNSName(b.Cfg.BaseDomain)
	}

	primary := hosts[svc.Name]

	for _, node := range b.State.ListNodes().All() {
		announced, ok := node.Services().Valid(), false

		var reported tailcfg.VIPService

		if announced {
			reported, ok = node.Services().AsStruct().Announced(svc.Name)
		}

		approved := node.ApprovedServices().ContainsFunc(func(n string) bool { return n == string(svc.Name) })
		if !ok && !approved {
			continue
		}

		out.Hosts = append(out.Hosts, Host{
			NodeID:    node.StringID(),
			Name:      node.GivenName(),
			Announced: ok,
			Ports:     nonNilStrings(types.ServicePortsStrings(reported.Ports)),
			Active:    reported.Active,
			Approved:  approved,
			Primary:   primary == node.ID(),
		})
	}

	return out
}

func registerServices(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listServices",
		Method:      http.MethodGet,
		Path:        "/api/v1/services",
		Summary:     "List services",
		Description: "The tailnet's services with the nodes that announce or may host each.",
		Tags:        []string{tagServices},
		Security:    bearerAuth,
	}, scope.ServicesRead), func(_ context.Context, _ *struct{}) (*listServicesOutput, error) {
		hosts := b.State.ServiceHosts()
		out := &listServicesOutput{}
		out.Body.Services = []Service{}

		for _, svc := range b.State.VIPServices() {
			out.Body.Services = append(out.Body.Services, b.serviceFrom(svc, hosts))
		}

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createService",
		Method:      http.MethodPost,
		Path:        "/api/v1/services",
		Summary:     "Create service",
		Description: "Creates a service and gives it a pair of tailnet addresses. Nodes host it once " +
			"they announce it and are approved.",
		Tags:          []string{tagServices},
		Security:      bearerAuth,
		DefaultStatus: http.StatusCreated,
	}, scope.Services), "service.create", "service", ""), func(
		ctx context.Context, in *createServiceInput,
	) (*serviceOutput, error) {
		svc, c, err := b.State.CreateVIPService(in.Body.Name, in.Body.DisplayName, in.Body.Comment, in.Body.Ports)
		if err != nil {
			return nil, mapError("creating service", err)
		}

		audit.Target(ctx, "service", strconv.FormatUint(svc.ID.Uint64(), 10), string(svc.Name))
		b.Change(c)

		out := &serviceOutput{}
		out.Body.Service = b.serviceFrom(svc, b.State.ServiceHosts())

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getService",
		Method:      http.MethodGet,
		Path:        "/api/v1/service/{name}",
		Summary:     "Get service",
		Tags:        []string{tagServices},
		Security:    bearerAuth,
	}, scope.ServicesRead), func(_ context.Context, in *serviceByNameInput) (*serviceOutput, error) {
		svc, err := b.getService(in.Name)
		if err != nil {
			return nil, err
		}

		out := &serviceOutput{}
		out.Body.Service = b.serviceFrom(svc, b.State.ServiceHosts())

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateService",
		Method:      http.MethodPut,
		Path:        "/api/v1/service/{name}",
		Summary:     "Update service",
		Tags:        []string{tagServices},
		Security:    bearerAuth,
	}, scope.Services), "service.update", "service", "name"), func(
		ctx context.Context, in *updateServiceInput,
	) (*serviceOutput, error) {
		name, err := types.ParseServiceName(in.Name)
		if err != nil {
			return nil, huma.Error400BadRequest("updating service", err)
		}

		svc, c, err := b.State.UpdateVIPService(name, in.Body.DisplayName, in.Body.Comment, in.Body.Ports)
		if err != nil {
			return nil, mapError("updating service", err)
		}

		audit.Target(ctx, "service", strconv.FormatUint(svc.ID.Uint64(), 10), string(svc.Name))
		b.Change(c)

		out := &serviceOutput{}
		out.Body.Service = b.serviceFrom(svc, b.State.ServiceHosts())

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteService",
		Method:      http.MethodDelete,
		Path:        "/api/v1/service/{name}",
		Summary:     "Delete service",
		Description: "Removes the service, frees its addresses and withdraws every host's approval.",
		Tags:        []string{tagServices},
		Security:    bearerAuth,
	}, scope.Services), "service.delete", "service", "name"), func(
		ctx context.Context, in *serviceByNameInput,
	) (*struct{}, error) {
		svc, err := b.getService(in.Name)
		if err != nil {
			return nil, err
		}

		c, err := b.State.DeleteVIPService(svc.Name)
		if err != nil {
			return nil, mapError("deleting service", err)
		}

		audit.Target(ctx, "service", strconv.FormatUint(svc.ID.Uint64(), 10), string(svc.Name))
		b.Change(c)

		return nil, nil //nolint:nilnil // 204 No Content
	})
}

// registerServiceApprovals registers the node-side endpoint for setting
// which services a node may host; kept separate from registerServices to
// stay under the function length limit.
func registerServiceApprovals(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setApprovedServices",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/approve_services",
		Summary:     "Set approved services",
		Description: "Replaces the services the node may host. Only a tagged node can host a service.",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.Services), "node.services.set", "node", "nodeId"), func(
		ctx context.Context, in *setApprovedServicesInput,
	) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "services", in.Body.Services)

		node, c, err := b.State.SetApprovedServices(nodeID, in.Body.Services)
		if err != nil {
			return nil, mapError("approving services", err)
		}

		audit.Target(ctx, "node", node.StringID(), node.GivenName())
		b.Change(c)

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)

		return out, nil
	})
}

func (b Backend) getService(raw string) (types.VIPService, error) {
	name, err := types.ParseServiceName(raw)
	if err != nil {
		return types.VIPService{}, huma.Error400BadRequest("parsing service name", err)
	}

	svc, err := b.State.GetVIPService(name)
	if err != nil {
		return types.VIPService{}, mapError("looking up service", err)
	}

	return svc, nil
}
