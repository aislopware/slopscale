package apiv2

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerUsers)
}

// Slopscale models no user type and has a single tailnet, so those fields
// are fixed strings: every account is a member of tailnet 1. The role is
// the user's real admin role and the status reflects users approval.
const (
	userTypeMember          = "member"
	userStatusActive        = "active"
	userStatusNeedsApproval = "needs-approval"
	singleTailnetID         = "1"

	// tagTailscaleCompat marks operations ported from the Tailscale API.
	tagTailscaleCompat = "Tailscale compat"
)

// User is the Tailscale user response. Identity fields map from the Slopscale
// user; type/status/tailnetId are constants (see above); role is the user's
// admin role; the device fields are aggregated from the user's nodes.
type User struct {
	ID                 string    `json:"id"`
	DisplayName        string    `json:"displayName"`
	LoginName          string    `json:"loginName"`
	ProfilePicURL      string    `json:"profilePicUrl"`
	TailnetID          string    `json:"tailnetId"`
	Created            time.Time `json:"created"`
	Type               string    `json:"type"`
	Role               string    `json:"role"`
	Status             string    `json:"status"`
	DeviceCount        int       `json:"deviceCount"`
	LastSeen           time.Time `json:"lastSeen"`
	CurrentlyConnected bool      `json:"currentlyConnected"`
}

type (
	userByIDInput struct {
		UserID string `doc:"User id (the decimal user id)." path:"id"`
	}
	listUsersInput struct {
		Tailnet string `doc:"Tailnet; must be \"-\" (the single Slopscale tailnet)."                  path:"tailnet"`
		Type    string `doc:"Filter by user type; Slopscale users are all \"member\"."                query:"type"`
		Role    string `doc:"Filter by role: owner, admin, network-admin, it-admin, auditor, member." query:"role"`
	}

	userOutput      struct{ Body User }
	listUsersOutput struct {
		Body struct {
			Users []User `json:"users" nullable:"false"`
		}
	}
)

func registerUsers(api huma.API, b Backend) {
	usersTags := []string{"Users", tagTailscaleCompat}

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getUser",
		Method:      http.MethodGet,
		Path:        "/api/v2/users/{id}",
		Summary:     "Get a user",
		Tags:        usersTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.UsersRead), func(_ context.Context, in *userByIDInput) (*userOutput, error) {
		view, err := lookupUser(b, in.UserID)
		if err != nil {
			return nil, err
		}

		return &userOutput{Body: userFromView(b, view)}, nil
	})

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "listUsers",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/users",
		Summary:     "List users",
		Tags:        usersTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.UsersRead), func(_ context.Context, in *listUsersInput) (*listUsersOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		out := &listUsersOutput{}
		out.Body.Users = []User{}

		// Slopscale has only "member"-type users. A filter for any other type,
		// or for a role that is not one, matches nothing, so return the empty
		// envelope.
		if !matchesMember(in.Type) || (in.Role != "" && !types.Role(in.Role).Valid()) {
			return out, nil
		}

		users, err := b.State.ListUsersWithFilter(&types.User{Role: types.Role(in.Role)})
		if err != nil {
			return nil, huma.Error500InternalServerError("listing users", err)
		}

		out.Body.Users = make([]User, 0, len(users))
		for i := range users {
			out.Body.Users = append(out.Body.Users, userFromView(b, users[i].View()))
		}

		return out, nil
	})

	registerUserApproval(api, b, usersTags)
}

// registerUserApproval registers the Tailscale approve/suspend/restore
// user actions. Suspend and restore map onto withdrawing and re-granting
// approval: a suspended user cannot register nodes and their nodes lose
// their peers until restored.
func registerUserApproval(api huma.API, b Backend, usersTags []string) {
	actions := []struct {
		id, path, summary string
		approved          bool
	}{
		{"approveUser", "approve", "Approve a user", true},
		{"suspendUser", "suspend", "Suspend a user", false},
		{"restoreUser", "restore", "Restore a user", true},
	}

	for _, action := range actions {
		huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
			OperationID:   action.id,
			Method:        http.MethodPost,
			Path:          "/api/v2/users/{id}/" + action.path,
			Summary:       action.summary,
			Tags:          usersTags,
			Security:      security,
			DefaultStatus: http.StatusOK,
			Errors:        []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
		}, scope.Users), "user.approval.set", "user", "id"), func(
			ctx context.Context, in *userByIDInput,
		) (*emptyOutput, error) {
			view, err := lookupUser(b, in.UserID)
			if err != nil {
				return nil, err
			}

			audit.Target(ctx, "", "", view.Name())
			audit.Detail(ctx, "approved", action.approved)

			_, userChange, err := b.State.SetUserApproval(types.UserID(view.ID()), action.approved)
			if err != nil {
				return nil, mapError(action.summary, err)
			}

			b.Change(userChange)

			return &emptyOutput{}, nil
		})
	}
}

// lookupUser resolves a user id to its UserView, mapping a malformed or unknown
// id to 404 (the Tailscale SDK keys IsNotFound off the status code), exactly as
// lookupNode does for devices.
func lookupUser(b Backend, rawID string) (types.UserView, error) {
	id, err := parseID(rawID, "user")
	if err != nil {
		return types.UserView{}, err
	}

	user, err := b.State.GetUserByID(types.UserID(id))
	if err != nil {
		return types.UserView{}, mapError("looking up user", err)
	}

	return user.View(), nil
}

// matchesMember reports whether an optional type filter selects Slopscale's
// only user type. An empty value means "no filter".
func matchesMember(filter string) bool {
	return filter == "" || filter == userTypeMember
}

// userFromView maps a Slopscale user onto the Tailscale User through the
// UserView accessors. deviceCount, lastSeen, and currentlyConnected are
// aggregated from the user's nodes in the NodeStore.
func userFromView(b Backend, view types.UserView) User {
	u := User{
		ID:            strconv.FormatUint(uint64(view.ID()), 10),
		DisplayName:   view.Display(),
		LoginName:     view.Username(),
		ProfilePicURL: view.ProfilePicURL(),
		TailnetID:     singleTailnetID,
		Created:       view.CreatedAt(),
		Type:          userTypeMember,
		Role:          view.Role().String(),
		Status:        userStatusActive,
	}

	if !view.ApprovedAt().Valid() {
		u.Status = userStatusNeedsApproval
	}

	nodes := b.State.ListNodesByUser(types.UserID(view.ID()))
	u.DeviceCount = nodes.Len()

	for _, node := range nodes.All() {
		if node.IsOnline().Valid() && node.IsOnline().Get() {
			u.CurrentlyConnected = true
		}

		if ls := node.LastSeen(); ls.Valid() && ls.Get().After(u.LastSeen) {
			u.LastSeen = ls.Get()
		}
	}

	return u
}
