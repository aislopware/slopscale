package apiv1

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerApps)
}

const tagApps = "Apps"

// App is a set of domains reached through app connectors, following
// Tailscale's app connectors. See docs/ref/apps.md.
type App struct {
	ID          string    `format:"uint64"                                                                   json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Domains     []string  `doc:"The domains the connectors resolve and route, example.com or *.example.com." json:"domains"    nullable:"false"` //nolint:lll // struct tag
	Connectors  []string  `doc:"The tags of the connector nodes, or * for every node running the connector." json:"connectors" nullable:"false"` //nolint:lll // struct tag
	Routes      []string  `doc:"Routes the connectors always advertise, next to the addresses they learn."   json:"routes"     nullable:"false"` //nolint:lll // struct tag
	Nodes       []AppNode `doc:"The connector nodes the selectors pick, with the routes they advertise."     json:"nodes"      nullable:"false"` //nolint:lll // struct tag
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AppNode is a connector node's standing towards an app.
type AppNode struct {
	NodeID string `format:"uint64"              json:"nodeId"`
	Name   string `doc:"The node's given name." json:"name"`
	Online bool   `json:"online"`
	// Connector reports whether the client runs the app connector service
	// (tailscale set --advertise-connector).
	Connector bool `doc:"true while the client reports running the connector service." json:"connector"`
	// LearnedRoutes counts the single-address routes the node advertises,
	// for every app it serves: only the node itself knows which address
	// belongs to which domain (GET /node/{id}/app-connector-routes asks
	// it). Pending counts the advertised routes nobody has approved yet.
	LearnedRoutes int `doc:"Single-address routes the node advertises."   json:"learnedRoutes"`
	Pending       int `doc:"Advertised routes that are not approved yet." json:"pending"`
}

// AppRequestBody creates or replaces an app.
type AppRequestBody struct {
	Name        string   `json:"name"                                                     minLength:"1"`
	Description string   `json:"description,omitempty"`
	Domains     []string `doc:"example.com or *.example.com."                             json:"domains,omitempty"`
	Connectors  []string `doc:"Tags of the connector nodes; empty means all connectors."  json:"connectors,omitempty"`
	Routes      []string `doc:"CIDRs the connectors always advertise; no default routes." json:"routes,omitempty"`
}

type (
	appIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	appBodyInput struct {
		Body AppRequestBody
	}
	appUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body AppRequestBody
	}
	appOutput struct {
		Body struct {
			App App `json:"app"`
		}
	}
	listAppsOutput struct {
		Body struct {
			Apps []App `json:"apps" nullable:"false"`
		}
	}
)

func parseAppID(s string) (types.AppConnectorID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid app id", err)
	}

	return types.AppConnectorID(id), nil
}

func appFromBody(body AppRequestBody) (types.AppConnector, error) {
	routes, err := types.ParseAppConnectorRoutes(body.Routes)
	if err != nil {
		return types.AppConnector{}, huma.Error400BadRequest("parsing app routes", err)
	}

	return types.AppConnector{
		Name:        body.Name,
		Description: body.Description,
		Domains:     body.Domains,
		Connectors:  body.Connectors,
		Routes:      routes,
	}, nil
}

func (b Backend) appFrom(app types.AppConnector) App {
	out := App{
		ID:          app.ID.String(),
		Name:        app.Name,
		Description: app.Description,
		Domains:     nonNilStrings(app.Domains),
		Connectors:  nonNilStrings(app.Connectors),
		Routes:      nonNilStrings(util.PrefixesToString(app.Routes)),
		Nodes:       []AppNode{},
		CreatedAt:   app.CreatedAt,
		UpdatedAt:   app.UpdatedAt,
	}

	for _, node := range b.State.AppConnectorNodes(app) {
		an := AppNode{
			NodeID:    node.StringID(),
			Name:      node.GivenName(),
			Online:    node.IsOnline().Valid() && node.IsOnline().Get(),
			Connector: node.Hostinfo().Valid() && node.Hostinfo().AppConnector().EqualBool(true),
		}

		approved := node.ApprovedRoutes().AsSlice()

		for _, r := range node.AnnouncedRoutes() {
			if r.IsSingleIP() {
				an.LearnedRoutes++
			}

			if !slices.Contains(approved, r) {
				an.Pending++
			}
		}

		out.Nodes = append(out.Nodes, an)
	}

	return out
}

func registerApps(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listApps",
		Method:      http.MethodGet,
		Path:        "/api/v1/apps",
		Summary:     "List apps",
		Description: "Apps are domains reached through app connectors: the connector nodes resolve the " +
			"domains, advertise a route for every address they learn and forward the traffic.",
		Tags:     []string{tagApps},
		Security: bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, _ *struct{}) (*listAppsOutput, error) {
		out := &listAppsOutput{}
		out.Body.Apps = []App{}

		for _, app := range b.State.AppConnectors() {
			out.Body.Apps = append(out.Body.Apps, b.appFrom(app))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getApp",
		Method:      http.MethodGet,
		Path:        "/api/v1/app/{id}",
		Summary:     "Get app",
		Tags:        []string{tagApps},
		Security:    bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, in *appIDInput) (*appOutput, error) {
		id, err := parseAppID(in.ID)
		if err != nil {
			return nil, err
		}

		app, err := b.State.GetAppConnector(id)
		if err != nil {
			return nil, mapError("getting app", err)
		}

		out := &appOutput{}
		out.Body.App = b.appFrom(app)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID:   "createApp",
		Method:        http.MethodPost,
		Path:          "/api/v1/apps",
		Summary:       "Create app",
		Description:   "Creates an app. Its connectors get the definition at once and may approve learned routes.",
		Tags:          []string{tagApps},
		Security:      bearerAuth,
		DefaultStatus: http.StatusCreated,
	}, scope.PolicyFile), "app.create", "app", ""), func(ctx context.Context, in *appBodyInput) (*appOutput, error) {
		app, err := appFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		created, c, err := b.State.CreateAppConnector(app)
		if err != nil {
			return nil, mapError("creating app", err)
		}

		return b.appResponse(ctx, created, c), nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateApp",
		Method:      http.MethodPut,
		Path:        "/api/v1/app/{id}",
		Summary:     "Update app",
		Description: "Replaces the app's definition.",
		Tags:        []string{tagApps},
		Security:    bearerAuth,
	}, scope.PolicyFile), "app.update", "app", "id"), func(
		ctx context.Context, in *appUpdateInput,
	) (*appOutput, error) {
		id, err := parseAppID(in.ID)
		if err != nil {
			return nil, err
		}

		app, err := appFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		app.ID = id

		updated, c, err := b.State.UpdateAppConnector(app)
		if err != nil {
			return nil, mapError("updating app", err)
		}

		return b.appResponse(ctx, updated, c), nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteApp",
		Method:      http.MethodDelete,
		Path:        "/api/v1/app/{id}",
		Summary:     "Delete app",
		Description: "Removes the app. Routes its connectors already had approved stay approved on the nodes.",
		Tags:        []string{tagApps},
		Security:    bearerAuth,
	}, scope.PolicyFile), "app.delete", "app", "id"), func(ctx context.Context, in *appIDInput) (*struct{}, error) {
		id, err := parseAppID(in.ID)
		if err != nil {
			return nil, err
		}

		app, err := b.State.GetAppConnector(id)
		if err != nil {
			return nil, mapError("deleting app", err)
		}

		c, err := b.State.DeleteAppConnector(id)
		if err != nil {
			return nil, mapError("deleting app", err)
		}

		audit.Target(ctx, "app", app.ID.String(), app.Name)
		b.Change(c)

		return nil, nil //nolint:nilnil // 204 No Content
	})
}

func (b Backend) appResponse(ctx context.Context, app types.AppConnector, c change.Change) *appOutput {
	audit.Target(ctx, "app", app.ID.String(), app.Name)
	b.Change(c)

	out := &appOutput{}
	out.Body.App = b.appFrom(app)

	return out
}
