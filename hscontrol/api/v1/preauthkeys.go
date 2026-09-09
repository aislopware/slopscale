package apiv1

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/api/tagguard"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerPreAuthKeys)
}

// PreAuthKey is the v1 PreAuthKey message. User is a pointer with no omitempty
// so tagged (system-created) keys emit "user":null. Expiration and CreatedAt
// are always emitted, zero-stamped when unset.
type PreAuthKey struct {
	User       *User     `json:"user"`
	ID         string    `format:"uint64"   json:"id"`
	Key        string    `json:"key"`
	Reusable   bool      `json:"reusable"`
	Ephemeral  bool      `json:"ephemeral"`
	Used       bool      `json:"used"`
	Expiration time.Time `json:"expiration"`
	CreatedAt  time.Time `json:"createdAt"`
	ACLTags    []string  `json:"aclTags"    nullable:"false"`

	Preauthorized bool     `doc:"Registered nodes skip device approval." json:"preauthorized"`
	GroupIDs      []string `doc:"Groups the registered node joins."      json:"groupIds"      nullable:"false"`
}

// CreatePreAuthKeyRequestBody is the v1.CreatePreAuthKeyRequest body. Every
// field is optional, hence omitempty throughout.
type CreatePreAuthKeyRequestBody struct {
	User       string     `format:"uint64"             json:"user,omitempty"`
	Reusable   bool       `json:"reusable,omitempty"`
	Ephemeral  bool       `json:"ephemeral,omitempty"`
	Expiration *time.Time `json:"expiration,omitempty"`
	ACLTags    []string   `json:"aclTags,omitempty"`

	Preauthorized *bool    `doc:"Defaults to true."                            json:"preauthorized,omitempty"`
	GroupIDs      []string `doc:"Groups a node registered with the key joins." json:"groupIds,omitempty"`
}

// ExpirePreAuthKeyRequestBody is the v1.ExpirePreAuthKeyRequest body.
type ExpirePreAuthKeyRequestBody struct {
	ID string `format:"uint64" json:"id,omitempty"`
}

type (
	createPreAuthKeyInput struct {
		Body CreatePreAuthKeyRequestBody
	}
	preAuthKeyOutput struct {
		Body struct {
			PreAuthKey PreAuthKey `json:"preAuthKey"`
		}
	}
)

type (
	expirePreAuthKeyInput struct {
		Body ExpirePreAuthKeyRequestBody
	}
	expirePreAuthKeyOutput struct {
		Body struct{}
	}
)

type (
	deletePreAuthKeyInput struct {
		ID string `format:"uint64" query:"id"`
	}
	deletePreAuthKeyOutput struct {
		Body struct{}
	}
)

type listPreAuthKeysOutput struct {
	Body struct {
		PreAuthKeys []PreAuthKey `json:"preAuthKeys" nullable:"false"`
	}
}

func registerPreAuthKeys(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createPreAuthKey",
		Method:      http.MethodPost,
		Path:        "/api/v1/preauthkey",
		Summary:     "Create pre-auth key",
		Tags:        []string{"PreAuthKeys"},
		Security:    bearerAuth,
	}, scope.AuthKeys), "preauthkey.create", "preauthkey", ""), func(
		ctx context.Context, in *createPreAuthKeyInput,
	) (*preAuthKeyOutput, error) {
		return handleCreatePreAuthKey(ctx, b, in)
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "expirePreAuthKey",
		Method:      http.MethodPost,
		Path:        "/api/v1/preauthkey/expire",
		Summary:     "Expire pre-auth key",
		Tags:        []string{"PreAuthKeys"},
		Security:    bearerAuth,
	}, scope.AuthKeys), "preauthkey.expire", "preauthkey", ""), func(
		ctx context.Context, in *expirePreAuthKeyInput,
	) (*expirePreAuthKeyOutput, error) {
		id, err := parsePreAuthKeyID(in.Body.ID)
		if err != nil {
			return nil, err
		}

		audit.Target(ctx, "", formatID(id), "")

		err = b.State.ExpirePreAuthKey(id)
		if err != nil {
			// An unknown key id maps to 404.
			return nil, mapError("expiring pre-auth key", err)
		}

		return &expirePreAuthKeyOutput{}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deletePreAuthKey",
		Method:      http.MethodDelete,
		Path:        "/api/v1/preauthkey",
		Summary:     "Delete pre-auth key",
		Tags:        []string{"PreAuthKeys"},
		Security:    bearerAuth,
	}, scope.AuthKeys), "preauthkey.delete", "preauthkey", ""), func(
		ctx context.Context, in *deletePreAuthKeyInput,
	) (*deletePreAuthKeyOutput, error) {
		// DELETE has no body: id is bound from the query string.
		id, err := parsePreAuthKeyID(in.ID)
		if err != nil {
			return nil, err
		}

		audit.Target(ctx, "", formatID(id), "")

		err = b.State.DeletePreAuthKey(id)
		if err != nil {
			// An unknown key id maps to 404.
			return nil, mapError("deleting pre-auth key", err)
		}

		return &deletePreAuthKeyOutput{}, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listPreAuthKeys",
		Method:      http.MethodGet,
		Path:        "/api/v1/preauthkey",
		Summary:     "List pre-auth keys",
		Tags:        []string{"PreAuthKeys"},
		Security:    bearerAuth,
	}, scope.AuthKeysRead), func(_ context.Context, _ *struct{}) (*listPreAuthKeysOutput, error) {
		preAuthKeys, err := b.State.ListPreAuthKeys()
		if err != nil {
			return nil, huma.Error500InternalServerError("listing pre-auth keys", err)
		}

		// Match the gRPC handler's ascending-ID ordering.
		slices.SortFunc(preAuthKeys, func(a, b types.PreAuthKey) int {
			return cmp.Compare(a.ID, b.ID)
		})

		out := &listPreAuthKeysOutput{}

		out.Body.PreAuthKeys = make([]PreAuthKey, len(preAuthKeys))
		for i := range preAuthKeys {
			out.Body.PreAuthKeys[i] = preAuthKeyToResponse(&preAuthKeys[i])
		}

		return out, nil
	})
}

// preAuthKeyNewToResponse builds the v1 response for a freshly created key. The
// plaintext key is returned only here; Used is always false.
func preAuthKeyNewToResponse(key *types.PreAuthKeyNew) PreAuthKey {
	out := PreAuthKey{
		ID:        formatID(key.ID),
		Key:       key.Key,
		Reusable:  key.Reusable,
		Ephemeral: key.Ephemeral,
		ACLTags:   nonNilTags(key.Tags),

		Preauthorized: key.Preauthorized,
		GroupIDs:      groupIDStrings(key.Groups),
	}

	if key.User != nil {
		u := userFromView(key.User.View())
		out.User = &u
	}

	if key.Expiration != nil {
		out.Expiration = *key.Expiration
	}

	if key.CreatedAt != nil {
		out.CreatedAt = *key.CreatedAt
	}

	return out
}

// groupIDStrings renders group ids for a response, never null.
func groupIDStrings(ids []types.GroupID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, formatID(uint64(id)))
	}

	return out
}

// preAuthKeyToResponse builds the v1 response for a stored key, with its key
// field masked (see maskedPreAuthKey).
func preAuthKeyToResponse(key *types.PreAuthKey) PreAuthKey {
	out := PreAuthKey{
		ID:        formatID(key.ID),
		Key:       maskedPreAuthKey(key.View()),
		Reusable:  key.Reusable,
		Ephemeral: key.Ephemeral,
		Used:      key.Used,
		ACLTags:   nonNilTags(key.Tags),

		Preauthorized: key.Preauthorized,
		GroupIDs:      groupIDStrings(key.Groups),
	}

	if key.User != nil {
		u := userFromView(key.User.View())
		out.User = &u
	}

	if key.Expiration != nil {
		out.Expiration = *key.Expiration
	}

	if key.CreatedAt != nil {
		out.CreatedAt = *key.CreatedAt
	}

	return out
}

// maskedPreAuthKey masks new keys (those with a stored prefix) so the secret is
// never returned; legacy plaintext keys are returned in full for backwards
// compatibility.
func maskedPreAuthKey(key types.PreAuthKeyView) string {
	if key.Prefix() != "" {
		return "hskey-auth-" + key.Prefix() + "-***"
	}

	return key.Key()
}

// nonNilTags ensures aclTags serializes as [] rather than null, matching
// EmitUnpopulated output.
func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}

	return tags
}

// parsePreAuthKeyUser parses the optional uint64 user field. Empty means "no
// user" (user 0); non-numeric is rejected with 400.
func parsePreAuthKeyUser(s string) (types.UserID, error) {
	if s == "" {
		return 0, nil
	}

	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid user id", err)
	}

	return types.UserID(id), nil
}

// parsePreAuthKeyID parses the uint64 key id. Empty means id 0; non-numeric is
// rejected with 400.
func parsePreAuthKeyID(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}

	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid pre-auth key id", err)
	}

	return id, nil
}

// keyGroups parses and checks the groups a key should enrol nodes in, and
// records them on the audit event when there are any.
func keyGroups(ctx context.Context, b Backend, ids []string) ([]types.GroupID, error) {
	groupIDs, err := parseGroupIDs("groupIds", ids)
	if err != nil {
		return nil, err
	}

	for _, gid := range groupIDs {
		_, getErr := b.State.GetGroup(gid)
		if getErr != nil {
			return nil, mapError("creating pre-auth key", getErr)
		}
	}

	if len(groupIDs) > 0 {
		audit.Detail(ctx, "groupIds", ids)
	}

	return groupIDs, nil
}

// handleCreatePreAuthKey mints a pre-auth key. Ownership is tags XOR user:
// tags make a tagged key, no tags make a key owned by the named user, or by
// the calling credential's own user.
func handleCreatePreAuthKey(ctx context.Context, b Backend, in *createPreAuthKeyInput) (*preAuthKeyOutput, error) {
	user, err := parsePreAuthKeyUser(in.Body.User)
	if err != nil {
		return nil, err
	}

	err = validateKeyTags(b, in.Body.ACLTags)
	if err != nil {
		return nil, err
	}

	err = authorizeKeyOwnership(ctx, b, in.Body.ACLTags, user)
	if err != nil {
		return nil, err
	}

	// CreatePreAuthKey requires a non-nil pointer; zero-stamp when unset.
	var expiration time.Time
	if in.Body.Expiration != nil {
		expiration = *in.Body.Expiration

		audit.Detail(ctx, "expiration", expiration.Format(time.RFC3339))
	}

	var userID *types.UserID

	if user != 0 {
		u, getErr := b.State.GetUserByID(user)
		if getErr != nil {
			return nil, mapError("creating pre-auth key", getErr)
		}

		userID = u.TypedID()

		audit.Detail(ctx, "userId", formatID(u.ID))
	}

	preauthorized := in.Body.Preauthorized == nil || *in.Body.Preauthorized

	groupIDs, err := keyGroups(ctx, b, in.Body.GroupIDs)
	if err != nil {
		return nil, err
	}

	audit.Detail(ctx, "reusable", in.Body.Reusable)
	audit.Detail(ctx, "ephemeral", in.Body.Ephemeral)
	audit.Detail(ctx, "preauthorized", preauthorized)
	audit.Detail(ctx, "tags", nonNilTags(in.Body.ACLTags))

	preAuthKey, err := b.State.CreatePreAuthKeyFromSpec(types.PreAuthKeySpec{
		UserID:        userID,
		Reusable:      in.Body.Reusable,
		Ephemeral:     in.Body.Ephemeral,
		Preauthorized: preauthorized,
		Expiration:    &expiration,
		Tags:          in.Body.ACLTags,
		Groups:        groupIDs,
	})
	if err != nil {
		// A key that is neither tagged nor user-owned is invalid input (400).
		return nil, mapError("creating pre-auth key", err)
	}

	// The created key has no prefix on hand, so it is named by its id;
	// the key itself is a secret and never audited.
	audit.Target(ctx, "", preAuthKey.StringID(), "")

	out := &preAuthKeyOutput{}
	out.Body.PreAuthKey = preAuthKeyNewToResponse(preAuthKey)

	return out, nil
}

// authorizeKeyOwnership gates who the key may belong to. An OAuth token acts
// for the tailnet through its tags: it may mint a tagged key only, only with
// tags its own tags own, and never a key that belongs to a user. v2 gates the
// same way (createAuthKey). Any other credential is bounded by its role.
func authorizeKeyOwnership(ctx context.Context, b Backend, tags []string, user types.UserID) error {
	var err error

	if len(tags) == 0 {
		err = tagguard.UntaggedKey(ctx)
	} else {
		err = tagguard.Assign(ctx, b.State, tags)
	}

	if err != nil {
		return err
	}

	if user != 0 {
		return tagguard.ActForUser(ctx, "create an auth key")
	}

	return nil
}

// validateKeyTags checks a pre-auth key's tags: the syntax, and that the
// policy defines each one, as [state.State.SetNodeTags] does. A key
// carrying an unknown tag mints a node no rule can name. A tailnet with
// no tagOwners at all keeps headscale's historical behaviour and takes
// any well-formed tag.
func validateKeyTags(b Backend, tags []string) error {
	err := validateTags(tags)
	if err != nil {
		return err
	}

	if !b.State.HasTagOwners() {
		return nil
	}

	for _, tag := range tags {
		if !b.State.TagExists(tag) {
			return huma.Error400BadRequest("tag " + tag + " is not defined in the policy")
		}
	}

	return nil
}

// validateTags rejects the first malformed tag as a 400.
func validateTags(tags []string) error {
	for _, tag := range tags {
		tagErr := validateTag(tag)
		if tagErr != nil {
			return huma.Error400BadRequest("invalid tag", tagErr)
		}
	}

	return nil
}
