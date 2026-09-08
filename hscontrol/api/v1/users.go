package apiv1

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerUsers, registerUserProfile, registerUserLifecycle, registerUserRole)
}

// CreateUserRequestBody mirrors v1.CreateUserRequest.
type CreateUserRequestBody struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	PictureURL  string `json:"pictureUrl,omitempty"`
}

type (
	createUserInput struct {
		Body CreateUserRequestBody
	}
	userOutput struct {
		Body struct {
			User User `json:"user"`
		}
	}
)

// UpdateUserRequestBody is the body of updateUser. A field left out keeps
// its value; an empty string clears it.
type UpdateUserRequestBody struct {
	DisplayName *string `doc:"The name shown in the clients in place of the username." json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	PictureURL  *string `doc:"The URL of the profile picture shown in the clients."    json:"pictureUrl,omitempty"`
}

type updateUserInput struct {
	ID   string `format:"uint64" path:"id"`
	Body UpdateUserRequestBody
}

// SetUserRoleRequestBody is the body of setUserRole.
type SetUserRoleRequestBody struct {
	Role string `doc:"One of owner, admin, network-admin, it-admin, auditor, member." json:"role"`
}

type (
	setUserRoleInput struct {
		ID   string `format:"uint64" path:"id"`
		Body SetUserRoleRequestBody
	}

	renameUserInput struct {
		OldID   string `format:"uint64" path:"oldId"`
		NewName string `path:"newName"`
	}

	deleteUserInput struct {
		ID string `format:"uint64" path:"id"`
	}
	deleteUserOutput struct {
		Body struct{}
	}
)

type (
	listUsersInput struct {
		ID    string `format:"uint64" query:"id"`
		Name  string `query:"name"`
		Email string `query:"email"`
	}
	listUsersOutput struct {
		Body struct {
			Users []User `json:"users" nullable:"false"`
		}
	}
)

func registerUsers(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/user",
		Summary:     "Create user",
		Tags:        []string{"Users"},
		Security:    bearerAuth,
	}, scope.Users), "user.create", "user", ""), func(ctx context.Context, in *createUserInput) (*userOutput, error) {
		// Pre-check yields a 409 for the common case; the DB unique constraint
		// is the real guard.
		if in.Body.Name != "" {
			_, err := b.State.GetUserByName(in.Body.Name)
			if err == nil {
				return nil, huma.Error409Conflict("user already exists")
			}
		}

		user, policyChanged, err := b.State.CreateUser(types.User{
			Name:          in.Body.Name,
			DisplayName:   in.Body.DisplayName,
			Email:         in.Body.Email,
			ProfilePicURL: in.Body.PictureURL,
		})
		if err != nil {
			return nil, mapError("creating user", err)
		}

		audit.Target(ctx, "", formatID(user.ID), user.Name)

		b.Change(policyChanged)

		out := &userOutput{}
		out.Body.User = userFromView(user.View())

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "renameUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/user/{oldId}/rename/{newName}",
		Summary:     "Rename user",
		Tags:        []string{"Users"},
		Security:    bearerAuth,
	}, scope.Users), "user.rename", "user", "oldId"), func(
		ctx context.Context, in *renameUserInput,
	) (*userOutput, error) {
		oldID, err := parseUserID(in.OldID)
		if err != nil {
			return nil, err
		}

		oldUser, err := b.State.GetUserByID(oldID)
		if err != nil {
			return nil, mapError("renaming user", err)
		}

		// The name the caller addressed, so the entry still names the user
		// as it was; newName carries the result.
		audit.Target(ctx, "", "", oldUser.Name)
		audit.Detail(ctx, "newName", in.NewName)

		_, c, err := b.State.RenameUser(types.UserID(oldUser.ID), in.NewName)
		if err != nil {
			return nil, mapError("renaming user", err)
		}

		b.Change(c)

		newUser, err := b.State.GetUserByName(in.NewName)
		if err != nil {
			return nil, huma.Error500InternalServerError("renaming user", err)
		}

		out := &userOutput{}
		out.Body.User = userFromView(newUser.View())

		return out, nil
	})
}

// registerUserProfile registers the operation that edits a user's profile.
func registerUserProfile(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateUser",
		Method:      http.MethodPatch,
		Path:        "/api/v1/user/{id}",
		Summary:     "Update user profile",
		Description: "Sets the display name, email or profile picture of a user. A field left out keeps its value; " +
			"an empty string clears it. The clients show the new profile on their next map update. " +
			"A user who logs in through OIDC gets the values from the provider again at the next login.",
		Tags:     []string{"Users"},
		Security: bearerAuth,
	}, scope.Users), "user.update", "user", "id"), func(
		ctx context.Context, in *updateUserInput,
	) (*userOutput, error) {
		return handleUpdateUser(ctx, b, in)
	})
}

// registerUserLifecycle registers the operations that delete and list users.
func registerUserLifecycle(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteUser",
		Method:      http.MethodDelete,
		Path:        "/api/v1/user/{id}",
		Summary:     "Delete user",
		Tags:        []string{"Users"},
		Security:    bearerAuth,
	}, scope.Users), "user.delete", "user", "id"), func(
		ctx context.Context, in *deleteUserInput,
	) (*deleteUserOutput, error) {
		id, err := parseUserID(in.ID)
		if err != nil {
			return nil, err
		}

		user, err := b.State.GetUserByID(id)
		if err != nil {
			return nil, mapError("deleting user", err)
		}

		audit.Target(ctx, "", "", user.Name)

		policyChanged, err := b.State.DeleteUser(types.UserID(user.ID))
		if err != nil {
			return nil, mapError("deleting user", err)
		}

		b.Change(policyChanged)

		return &deleteUserOutput{}, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listUsers",
		Method:      http.MethodGet,
		Path:        "/api/v1/user",
		Summary:     "List users",
		Tags:        []string{"Users"},
		Security:    bearerAuth,
	}, scope.UsersRead), func(_ context.Context, in *listUsersInput) (*listUsersOutput, error) {
		// Gateway parity: a non-numeric id is a 400 even when other filters win.
		if in.ID != "" {
			_, err := strconv.ParseUint(in.ID, 10, 64)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid id", err)
			}
		}

		users, err := listUsersFiltered(b, in)
		if err != nil {
			return nil, huma.Error500InternalServerError("listing users", err)
		}

		// Match the gRPC handler's ascending-ID ordering.
		slices.SortFunc(users, func(a, b types.User) int {
			return cmp.Compare(a.ID, b.ID)
		})

		out := &listUsersOutput{}

		out.Body.Users = make([]User, len(users))
		for i := range users {
			out.Body.Users[i] = userFromView(users[i].View())
		}

		return out, nil
	})
}

// listUsersFiltered reproduces the gRPC ListUsers precedence: name, then email,
// then id, otherwise all users.
func listUsersFiltered(b Backend, in *listUsersInput) ([]types.User, error) {
	switch {
	case in.Name != "":
		return b.State.ListUsersWithFilter(&types.User{Name: in.Name})
	case in.Email != "":
		return b.State.ListUsersWithFilter(&types.User{Email: in.Email})
	case in.ID != "":
		id, err := strconv.ParseUint(in.ID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing user id %q: %w", in.ID, err)
		}

		if id == 0 {
			return b.State.ListAllUsers()
		}

		return b.State.ListUsersWithFilter(&types.User{ID: uint(id)})
	default:
		return b.State.ListAllUsers()
	}
}

func parseUserID(s string) (types.UserID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid user id", err)
	}

	return types.UserID(id), nil
}

func registerUserRole(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setUserRole",
		Method:      http.MethodPost,
		Path:        "/api/v1/user/{id}/role",
		Summary:     "Set user role",
		Description: "Assigns an admin role. Only the owner or an admin may assign roles, nobody " +
			"may change their own, and assigning owner transfers ownership: the previous owner " +
			"becomes an admin. The owner's role changes only by such a transfer.",
		Tags:     []string{"Users"},
		Security: bearerAuth,
	}, scope.Users), "user.role.set", "user", "id"), func(
		ctx context.Context, in *setUserRoleInput,
	) (*userOutput, error) {
		id, err := parseUserID(in.ID)
		if err != nil {
			return nil, err
		}

		role, err := types.ParseRole(in.Body.Role)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid role", err)
		}

		audit.Detail(ctx, "role", role.String())

		user, policyChanged, err := b.State.SetUserRole(roleActor(ctx), id, role)
		if err != nil {
			return nil, mapError("setting user role", err)
		}

		audit.Target(ctx, "", "", user.Name)

		b.Change(policyChanged)

		out := &userOutput{}
		out.Body.User = userFromView(user.View())

		return out, nil
	})
}

// handleUpdateUser applies the profile fields the request carries and
// tells every client, whose map response takes the profile from the
// node's copy of its user.
func handleUpdateUser(ctx context.Context, b Backend, in *updateUserInput) (*userOutput, error) {
	id, err := parseUserID(in.ID)
	if err != nil {
		return nil, err
	}

	if in.Body.DisplayName == nil && in.Body.Email == nil && in.Body.PictureURL == nil {
		return nil, huma.Error400BadRequest("nothing to update")
	}

	user, c, err := b.State.UpdateUser(id, func(user *types.User) error {
		if in.Body.DisplayName != nil {
			user.DisplayName = *in.Body.DisplayName
		}

		if in.Body.Email != nil {
			user.Email = *in.Body.Email
		}

		if in.Body.PictureURL != nil {
			user.ProfilePicURL = *in.Body.PictureURL
		}

		return nil
	})
	if err != nil {
		return nil, mapError("updating user", err)
	}

	audit.Target(ctx, "", formatID(user.ID), user.Name)

	b.Change(c)

	out := &userOutput{}
	out.Body.User = userFromView(user.View())

	return out, nil
}
