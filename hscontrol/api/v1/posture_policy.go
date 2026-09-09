package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/posture"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerPostures, registerPostureExtras)
}

// Posture is a reusable set of conditions on a source machine. An access
// rule that names postures lets a source through when any one of them
// holds; within a posture every expression must hold and the schedule,
// when there is one, must be open.
type Posture struct {
	ID          string `format:"uint64"    json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Expressions are posture expressions such as "node:os == 'macos'",
	// "node:tsVersion >= '1.40'", "custom:oncall == true",
	// "ip:address IN ['203.0.113.0/24']" or "ip:country IN ['VN', 'SG']".
	Expressions []string         `json:"expressions"        nullable:"false"`
	Schedule    *PostureSchedule `json:"schedule,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

// PostureSchedule is a weekly window the posture holds in.
type PostureSchedule struct {
	Days     []string `doc:"Weekdays: mon, tue, wed, thu, fri, sat, sun." json:"days"               nullable:"false"`
	Start    string   `doc:"HH:MM in the time zone."                      json:"start"`
	End      string   `doc:"HH:MM; before start wraps past midnight."     json:"end"`
	Timezone string   `doc:"IANA zone name; empty means UTC."             json:"timezone,omitempty"`
}

// PostureRequestBody creates or replaces a posture.
type PostureRequestBody struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Expressions []string         `json:"expressions,omitempty"`
	Schedule    *PostureSchedule `json:"schedule,omitempty"`
}

type (
	postureIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	postureBodyInput struct {
		Body PostureRequestBody
	}
	postureUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body PostureRequestBody
	}
	postureOutput struct {
		Body struct {
			Posture Posture `json:"posture"`
		}
	}
	listPosturesOutput struct {
		Body struct {
			Postures []Posture `json:"postures" nullable:"false"`
			// GeoIPAvailable tells a client whether ip:country can be
			// used, which needs policy.geoip_database on the server.
			GeoIPAvailable bool `json:"geoIpAvailable"`
		}
	}
	postureCheckInput struct {
		Body struct {
			Expressions []string `json:"expressions"`
		}
	}
	postureCheckOutput struct {
		Body struct {
			// Errors has one entry per expression, empty when it parses.
			Errors []string `json:"errors" nullable:"false"`
		}
	}
	nodePosturesOutput struct {
		Body struct {
			Postures []Posture `json:"postures" nullable:"false"`
		}
	}
)

func postureFrom(p types.Posture) Posture {
	out := Posture{
		ID:          formatID(uint64(p.ID)),
		Name:        p.Name,
		Description: p.Description,
		Expressions: append([]string{}, p.Expressions...),
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}

	if p.Schedule != nil {
		out.Schedule = &PostureSchedule{
			Days:     append([]string{}, p.Schedule.Days...),
			Start:    p.Schedule.Start,
			End:      p.Schedule.End,
			Timezone: p.Schedule.Timezone,
		}
	}

	return out
}

func postureFromBody(body PostureRequestBody) types.Posture {
	p := types.Posture{
		Name:        body.Name,
		Description: body.Description,
		Expressions: body.Expressions,
	}

	if body.Schedule != nil {
		p.Schedule = &posture.Schedule{
			Days:     body.Schedule.Days,
			Start:    body.Schedule.Start,
			End:      body.Schedule.End,
			Timezone: body.Schedule.Timezone,
		}
	}

	return p
}

func parsePostureID(s string) (types.PostureID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid posture id", err)
	}

	return types.PostureID(id), nil
}

func parsePostureIDs(ids []string) ([]types.PostureID, error) {
	out := make([]types.PostureID, 0, len(ids))

	for _, s := range ids {
		id, err := parsePostureID(s)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid postureIds entry", err)
		}

		out = append(out, id)
	}

	return out, nil
}

func registerPostures(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listPostures",
		Method:      http.MethodGet,
		Path:        "/api/v1/posture",
		Summary:     "List postures",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, _ *struct{}) (*listPosturesOutput, error) {
		postures := b.State.ListPostures()

		out := &listPosturesOutput{}
		out.Body.GeoIPAvailable = b.State.GeoIPAvailable()
		out.Body.Postures = make([]Posture, 0, len(postures))

		for _, p := range postures {
			out.Body.Postures = append(out.Body.Postures, postureFrom(p))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getPosture",
		Method:      http.MethodGet,
		Path:        "/api/v1/posture/{id}",
		Summary:     "Get posture",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, in *postureIDInput) (*postureOutput, error) {
		id, err := parsePostureID(in.ID)
		if err != nil {
			return nil, err
		}

		p, err := b.State.GetPosture(id)
		if err != nil {
			return nil, mapError("getting posture", err)
		}

		out := &postureOutput{}
		out.Body.Posture = postureFrom(p)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createPosture",
		Method:      http.MethodPost,
		Path:        "/api/v1/posture",
		Summary:     "Create posture",
		Description: "A posture is a set of conditions a source machine must satisfy, checked by the access " +
			"rules that name it. Every expression must hold; a schedule limits the posture to a weekly window.",
		Tags:     []string{tagAccessControl},
		Security: bearerAuth,
	}, scope.PolicyFile), "posture.create", "posture", ""), func(
		ctx context.Context, in *postureBodyInput,
	) (*postureOutput, error) {
		created, c, err := b.State.CreatePosture(postureFromBody(in.Body))
		if err != nil {
			return nil, mapError("creating posture", err)
		}

		audit.Target(ctx, "", formatID(uint64(created.ID)), created.Name)
		b.Change(c)

		out := &postureOutput{}
		out.Body.Posture = postureFrom(created)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updatePosture",
		Method:      http.MethodPut,
		Path:        "/api/v1/posture/{id}",
		Summary:     "Replace posture",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "posture.update", "posture", "id"), func(
		ctx context.Context, in *postureUpdateInput,
	) (*postureOutput, error) {
		id, err := parsePostureID(in.ID)
		if err != nil {
			return nil, err
		}

		p := postureFromBody(in.Body)
		p.ID = id

		updated, c, err := b.State.UpdatePosture(p)
		if err != nil {
			return nil, mapError("updating posture", err)
		}

		audit.Target(ctx, "", "", updated.Name)
		b.Change(c)

		out := &postureOutput{}
		out.Body.Posture = postureFrom(updated)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deletePosture",
		Method:      http.MethodDelete,
		Path:        "/api/v1/posture/{id}",
		Summary:     "Delete posture",
		Description: "Refused while an access rule names the posture.",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "posture.delete", "posture", "id"), func(
		ctx context.Context, in *postureIDInput,
	) (*emptyOutput, error) {
		id, err := parsePostureID(in.ID)
		if err != nil {
			return nil, err
		}

		p, err := b.State.GetPosture(id)
		if err != nil {
			return nil, mapError("deleting posture", err)
		}

		c, err := b.State.DeletePosture(id)
		if err != nil {
			return nil, mapError("deleting posture", err)
		}

		audit.Target(ctx, "", "", p.Name)
		b.Change(c)

		return &emptyOutput{}, nil
	})
}

// registerPostureExtras adds the expression checker and the per-node
// match list.
func registerPostureExtras(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "checkPostureExpressions",
		Method:      http.MethodPost,
		Path:        "/api/v1/posture/check",
		Summary:     "Check posture expressions",
		Description: "Parses expressions without storing anything, for an editor to show errors as they are typed.",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, in *postureCheckInput) (*postureCheckOutput, error) {
		out := &postureCheckOutput{}
		out.Body.Errors = make([]string, 0, len(in.Body.Expressions))

		for _, e := range in.Body.Expressions {
			_, err := posture.Parse(e)
			if err != nil {
				out.Body.Errors = append(out.Body.Errors, err.Error())
			} else {
				out.Body.Errors = append(out.Body.Errors, "")
			}
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listNodePostures",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/postures",
		Summary:     "List the postures a node satisfies",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.DevicesPostureAttributesRead), func(_ context.Context, in *nodePostureInput) (*nodePosturesOutput, error) {
		id, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		postures, err := b.State.MatchingPostures(id)
		if err != nil {
			return nil, mapError("listing node postures", err)
		}

		out := &nodePosturesOutput{}
		out.Body.Postures = make([]Posture, 0, len(postures))

		for _, p := range postures {
			out.Body.Postures = append(out.Body.Postures, postureFrom(p))
		}

		return out, nil
	})
}
