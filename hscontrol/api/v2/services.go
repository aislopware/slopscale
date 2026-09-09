package apiv2

import (
	"context"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerServices)
}

var serviceTags = []string{"Services", tagTailscaleCompat}

// VIPService is Tailscale's service shape, what the Go client's
// ServicesResource reads and writes. Tags and annotations are accepted
// and ignored: slopscale approves hosts per node, and the display name
// travels as the "displayName" annotation.
type VIPService struct {
	Name        string            `json:"name"`
	Addrs       []string          `json:"addrs"                 nullable:"false"`
	Comment     string            `json:"comment,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Ports       []string          `json:"ports"                 nullable:"false"`
	Tags        []string          `json:"tags"                  nullable:"false"`
}

// PutVIPServiceRequest is the PUT body; name in the body, when given,
// must match the path.
type PutVIPServiceRequest struct {
	Name        string            `json:"name,omitempty"`
	Comment     string            `json:"comment,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Ports       []string          `json:"ports,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

type (
	listVIPServicesOutput struct {
		Body struct {
			Services []VIPService `json:"vipServices" nullable:"false"`
		}
	}
	vipServiceInput struct {
		Tailnet string `path:"tailnet"`
		Name    string `doc:"The service name, svc:<label>." path:"name"`
	}
	vipServiceOutput struct {
		Body VIPService
	}
	putVIPServiceInput struct {
		Tailnet string `path:"tailnet"`
		Name    string `path:"name"`
		Body    PutVIPServiceRequest
	}
)

// displayNameAnnotation carries the display name, which Tailscale's
// shape has no field for.
const displayNameAnnotation = "displayName"

// portsDoNotValidate is the sentinel the Kubernetes operator sends while
// it does not yet know which ports a service listens on. Tailscale takes
// it as "accept the service without checking its ports"; slopscale stores
// no ports for it, so the service reads back with an empty port list.
const portsDoNotValidate = "do-not-validate"

// putVIPService is the PUT create-or-update body for registerServices: it
// creates the service when it does not exist yet and updates it otherwise.
func (b Backend) putVIPService(ctx context.Context, in *putVIPServiceInput) (*vipServiceOutput, error) {
	err := requireDefaultTailnet(in.Tailnet)
	if err != nil {
		return nil, err
	}

	name, err := types.ParseServiceName(in.Name)
	if err != nil {
		return nil, huma.Error400BadRequest("parsing service name", err)
	}

	if in.Body.Name != "" {
		bodyName, bodyErr := types.ParseServiceName(in.Body.Name)
		if bodyErr != nil || bodyName != name {
			return nil, huma.Error400BadRequest("service name in the body differs from the path", bodyErr)
		}
	}

	displayName := in.Body.Annotations[displayNameAnnotation]
	ports := in.Body.Ports

	if len(ports) == 0 || (len(ports) == 1 && ports[0] == portsDoNotValidate) {
		ports = []string{}
	}

	var (
		svc types.VIPService
		c   change.Change
	)

	_, err = b.State.GetVIPService(name)
	if err != nil {
		svc, c, err = b.State.CreateVIPService(string(name), displayName, in.Body.Comment, ports)
	} else {
		svc, c, err = b.State.UpdateVIPService(name, &displayName, &in.Body.Comment, &ports)
	}

	if err != nil {
		return nil, mapError("storing service", err)
	}

	audit.Target(ctx, "service", "", string(svc.Name))
	b.Change(c)

	return &vipServiceOutput{Body: vipServiceFrom(svc)}, nil
}

func vipServiceFrom(svc types.VIPService) VIPService {
	out := VIPService{
		Name:    string(svc.Name),
		Addrs:   []string{},
		Comment: svc.Comment,
		Ports:   emptyIfNil(types.ServicePortsStrings(svc.Ports)),
		Tags:    []string{},
	}

	for _, addr := range svc.Addrs() {
		out.Addrs = append(out.Addrs, addr.String())
	}

	if svc.DisplayName != "" {
		out.Annotations = map[string]string{displayNameAnnotation: svc.DisplayName}
	}

	return out
}

func registerServices(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "listVIPServices",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/vip-services",
		Summary:     "List services",
		Tags:        serviceTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.ServicesRead), func(_ context.Context, in *tailnetInput) (*listVIPServicesOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		out := &listVIPServicesOutput{}
		out.Body.Services = []VIPService{}

		for _, svc := range b.State.VIPServices() {
			out.Body.Services = append(out.Body.Services, vipServiceFrom(svc))
		}

		return out, nil
	})

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getVIPService",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/vip-services/{name}",
		Summary:     "Get service",
		Tags:        serviceTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.ServicesRead), func(_ context.Context, in *vipServiceInput) (*vipServiceOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		name, err := types.ParseServiceName(in.Name)
		if err != nil {
			return nil, huma.Error400BadRequest("parsing service name", err)
		}

		svc, err := b.State.GetVIPService(name)
		if err != nil {
			return nil, mapError("getting service", err)
		}

		return &vipServiceOutput{Body: vipServiceFrom(svc)}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "putVIPService",
		Method:      http.MethodPut,
		Path:        "/api/v2/tailnet/{tailnet}/vip-services/{name}",
		Summary:     "Create or update service",
		Description: "Creates the service when it does not exist, giving it a pair of tailnet " +
			"addresses, and replaces its comment, ports and display name otherwise.",
		Tags:     serviceTags,
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.Services), "service.put", "service", "name"), b.putVIPService)

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "deleteVIPService",
		Method:      http.MethodDelete,
		Path:        "/api/v2/tailnet/{tailnet}/vip-services/{name}",
		Summary:     "Delete service",
		Tags:        serviceTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.Services), "service.delete", "service", "name"), func(
		ctx context.Context, in *vipServiceInput,
	) (*struct{}, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		name, err := types.ParseServiceName(in.Name)
		if err != nil {
			return nil, huma.Error400BadRequest("parsing service name", err)
		}

		c, err := b.State.DeleteVIPService(name)
		if err != nil {
			return nil, mapError("deleting service", err)
		}

		audit.Target(ctx, "service", "", string(name))
		b.Change(c)

		return nil, nil //nolint:nilnil // 204 No Content
	})
}
