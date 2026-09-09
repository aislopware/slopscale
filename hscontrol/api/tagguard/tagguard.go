// Package tagguard bounds what an OAuth access token may do with tags. A
// token is the tailnet's own credential: it acts through the tags it was
// granted, so it may only put those tags (or tags they own) on a node or a
// key, and it may not act for a user at all. Every other credential is
// bounded by its user's role instead and passes through here.
//
// Both API versions gate the same way, so the rules live here rather than
// once per version: v2 has enforced them since it shipped, v1 did not, and a
// second copy would be a second chance to drift.
package tagguard

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/api/principal"
)

// Policy is the tag ownership the guard reads; *state.State satisfies it.
type Policy interface {
	// TagExists reports whether the policy defines the tag.
	TagExists(tag string) bool
	// TagOwnedByTags reports whether a credential holding ownerTags may
	// apply tag.
	TagOwnedByTags(tag string, ownerTags []string) bool
}

// PrincipalTags returns the tags granted to the request's OAuth access token,
// and whether the request authenticated with one. Only a token is bounded by
// tags; an API key keeps the historical syntax-only tag validation.
func PrincipalTags(ctx context.Context) ([]string, bool) {
	p, ok := principal.From(ctx)
	if !ok {
		return nil, false
	}

	return p.Tags, p.IsOAuth()
}

// Assign checks the tags a request wants to put on a key or a node: each must
// be defined in the policy and owned by the token's own tags. It is a no-op
// for any other credential.
func Assign(ctx context.Context, pol Policy, tags []string) error {
	return assign(ctx, pol, tags, true)
}

// AssignOwned checks ownership only, for a caller whose state layer already
// refuses a tag the policy does not define ([state.State.SetNodeTags]).
func AssignOwned(ctx context.Context, pol Policy, tags []string) error {
	return assign(ctx, pol, tags, false)
}

func assign(ctx context.Context, pol Policy, tags []string, defined bool) error {
	tokenTags, isOAuth := PrincipalTags(ctx)
	if !isOAuth {
		return nil
	}

	for _, tag := range tags {
		if defined && !pol.TagExists(tag) {
			return huma.Error400BadRequest("tag " + tag + " is not defined in policy")
		}

		if !pol.TagOwnedByTags(tag, tokenTags) {
			return huma.Error403Forbidden("token may not assign tag " + tag)
		}
	}

	return nil
}

// UntaggedKey refuses an auth key with no tags to an OAuth token: such a key
// is owned by a user, and a token acts for no user.
func UntaggedKey(ctx context.Context) error {
	if _, isOAuth := PrincipalTags(ctx); isOAuth {
		return huma.Error403Forbidden("an OAuth client must create tagged auth keys")
	}

	return nil
}

// ActForUser refuses an OAuth token doing something on a named user's behalf,
// such as minting that user's auth key or registering a node into their
// account. action names it for the message ("create an auth key").
func ActForUser(ctx context.Context, action string) error {
	if _, isOAuth := PrincipalTags(ctx); isOAuth {
		return huma.Error403Forbidden("an OAuth client may not " + action + " for a user; use tags")
	}

	return nil
}
