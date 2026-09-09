package types

import (
	"errors"
	"time"
)

// InviteDefaultExpiry is how long an invite lasts when the request does
// not say.
const InviteDefaultExpiry = 7 * 24 * time.Hour

// InviteMaxExpiry bounds how long an invite may last: a link that creates
// an account with a role should not sit in a mailbox forever.
const InviteMaxExpiry = 30 * 24 * time.Hour

var (
	// ErrInviteNotFound is returned for an invite that does not exist.
	ErrInviteNotFound = errors.New("invite not found")
	// ErrInviteExpired is returned for an invite past its expiry.
	ErrInviteExpired = errors.New("invite expired")
	// ErrInviteAccepted is returned for an invite that was already used.
	ErrInviteAccepted = errors.New("invite already accepted")
	// ErrInviteEmailTaken is returned when the address already belongs to
	// a user or to a pending invite.
	ErrInviteEmailTaken = errors.New("the email already belongs to a user or a pending invite")
	// ErrInviteEmailEmpty is returned for an invite without an address.
	ErrInviteEmailEmpty = errors.New("an invite needs an email address")
	// ErrInviteEmailInvalid is returned for an address that is not a bare
	// mailbox: the invite link is mailed to it and the address becomes the
	// user's login, so a display name or a list is not an address here.
	ErrInviteEmailInvalid = errors.New("an invite needs a plain email address")
	// ErrInviteOwnerRole is returned when an invite would create an owner:
	// ownership moves by transfer, never by invitation.
	ErrInviteOwnerRole = errors.New("an invite cannot create an owner; transfer ownership instead")
	// ErrInviteExpiryRange is returned for an expiry outside the allowed
	// range.
	ErrInviteExpiryRange = errors.New("an invite must expire between now and 30 days from now")
)

// UserInviteID identifies an invite.
type UserInviteID uint64

// UserInvite is an invitation to join the tailnet. The invitee holds a
// random token in a link and the server keeps only its hash, as for a
// console session. The first login that presents the token, or whose
// verified email matches, creates the user with the role and groups the
// invite carries.
type UserInvite struct {
	ID       UserInviteID
	Email    string
	Role     Role
	GroupIDs []GroupID

	ExpiresAt time.Time
	CreatedAt time.Time
	// CreatedBy is the administrator who sent the invite; zero when the
	// invite was made by a credential without a user, or when that user
	// has since been deleted.
	CreatedBy UserID

	AcceptedAt *time.Time
	// AcceptedUserID is the user the invite created; zero until then.
	AcceptedUserID UserID
}

// Accepted reports whether the invite has been used.
func (i UserInvite) Accepted() bool {
	return i.AcceptedAt != nil
}

// Expired reports whether the invite is past its expiry at now.
func (i UserInvite) Expired(now time.Time) bool {
	return !now.Before(i.ExpiresAt)
}

// Pending reports whether the invite can still be accepted at now.
func (i UserInvite) Pending(now time.Time) bool {
	return !i.Accepted() && !i.Expired(now)
}
