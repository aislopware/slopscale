package types

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// AccessRequestID identifies a row of access_requests.
type AccessRequestID uint64

// String renders the ID in base 10.
func (id AccessRequestID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// AccessRequestStatus is where a request stands.
type AccessRequestStatus string

// The statuses a request moves through. A request is decided once and
// never reopened; the membership an approval made ends on its own, or
// early when an approver revokes it.
const (
	AccessRequestPending   AccessRequestStatus = "pending"
	AccessRequestApproved  AccessRequestStatus = "approved"
	AccessRequestDenied    AccessRequestStatus = "denied"
	AccessRequestCancelled AccessRequestStatus = "cancelled"
	AccessRequestRevoked   AccessRequestStatus = "revoked"
)

// AccessRequestStatuses lists every status, in lifecycle order.
var AccessRequestStatuses = []AccessRequestStatus{
	AccessRequestPending, AccessRequestApproved, AccessRequestDenied,
	AccessRequestCancelled, AccessRequestRevoked,
}

// AccessRequest is a user's ask to join a requestable group for a while,
// for one of their machines or for every machine they own. Approval adds
// the membership with an expiry and the request keeps the outcome.
type AccessRequest struct {
	ID AccessRequestID
	// UserID is the requester.
	UserID UserID
	// NodeID is the machine the access is for; nil means every machine
	// the user owns, now and later.
	NodeID  *NodeID
	GroupID GroupID
	Reason  string
	// Duration is how long the membership should last once approved.
	Duration time.Duration
	Status   AccessRequestStatus
	// DecidedBy names who approved or denied, for the record.
	DecidedBy string
	// Note is what the approver said when deciding.
	Note      string
	CreatedAt time.Time
	DecidedAt *time.Time
	// ExpiresAt is when the granted membership ends; set on approval. A
	// revoked request keeps it, as the record of what was approved, and
	// RevokedAt says when the access really ended.
	ExpiresAt *time.Time
	// RevokedBy, RevokedAt and RevokeNote record an approval ended early;
	// they leave the approval's own record in place.
	RevokedBy  string
	RevokedAt  *time.Time
	RevokeNote string
}

// Pending reports whether the request still waits for a decision.
func (r AccessRequest) Pending() bool {
	return r.Status == AccessRequestPending
}

// Active reports whether an approved request's membership still holds at
// the instant.
func (r AccessRequest) Active(now time.Time) bool {
	return r.Status == AccessRequestApproved && r.ExpiresAt != nil && now.Before(*r.ExpiresAt)
}

// Revoked reports whether an approval was ended early.
func (r AccessRequest) Revoked() bool {
	return r.Status == AccessRequestRevoked
}

// The bounds on how long a request may ask for.
const (
	MinAccessRequestDuration = 5 * time.Minute
	MaxAccessRequestDuration = 30 * 24 * time.Hour
	maxAccessRequestText     = 500
)

// Errors of the access request flow.
var (
	ErrAccessRequestNotFound          = errors.New("access request not found")
	ErrAccessRequestDecided           = errors.New("access request was already decided")
	ErrAccessRequestNoUser            = errors.New("an access request needs a user; the credential has none")
	ErrAccessRequestNotRequestable    = errors.New("the group does not take access requests")
	ErrAccessRequestDuration          = errors.New("access request duration must be between 5 minutes and 30 days")
	ErrAccessRequestTextTooLong       = errors.New("access request text must be at most 500 characters")
	ErrAccessRequestNodeOwner         = errors.New("the machine must belong to the requester")
	ErrAccessRequestOwn               = errors.New("nobody decides their own access request")
	ErrAccessRequestPendingExists     = errors.New("a request for the same group and machine is already pending")
	ErrAccessRequestNotActive         = errors.New("only access that is in effect can be revoked")
	ErrAccessRequestActive            = errors.New("revoke the access before deleting the request")
	ErrAccessRequestOverGroupMax      = errors.New("the group takes shorter requests")
	ErrGroupMaxRequestDuration        = errors.New("a group's longest request must be between 5 minutes and 30 days")
	ErrGroupAutoApproveNotRequestable = errors.New("only a requestable group can approve requests by itself")
)

// GroupRequests is how a group takes access requests.
type GroupRequests struct {
	// Requestable lets members ask to join the group for a while.
	Requestable bool
	// AutoApprove grants a request the moment it is filed, with no
	// approver; the group's cap on its length is the only check.
	AutoApprove bool
	// MaxDuration caps how long one request may ask for; zero leaves
	// only [MaxAccessRequestDuration].
	MaxDuration time.Duration
}

// Validate checks the settings an operator gives a group.
func (r GroupRequests) Validate() error {
	if r.AutoApprove && !r.Requestable {
		return ErrGroupAutoApproveNotRequestable
	}

	if r.MaxDuration != 0 &&
		(r.MaxDuration < MinAccessRequestDuration || r.MaxDuration > MaxAccessRequestDuration) {
		return ErrGroupMaxRequestDuration
	}

	return nil
}

// CheckDuration refuses a request longer than the group's cap.
func (r GroupRequests) CheckDuration(d time.Duration) error {
	if r.MaxDuration != 0 && d > r.MaxDuration {
		return fmt.Errorf("%w: at most %s", ErrAccessRequestOverGroupMax, r.MaxDuration)
	}

	return nil
}

// ValidateAccessRequestDuration checks the bounds.
func ValidateAccessRequestDuration(d time.Duration) error {
	if d < MinAccessRequestDuration || d > MaxAccessRequestDuration {
		return ErrAccessRequestDuration
	}

	return nil
}

// ValidateAccessRequestText bounds a reason or note.
func ValidateAccessRequestText(s string) error {
	if len([]rune(s)) > maxAccessRequestText {
		return ErrAccessRequestTextTooLong
	}

	return nil
}
