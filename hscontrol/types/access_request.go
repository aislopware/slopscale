package types

import (
	"errors"
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
// never reopened; the membership an approval made ends on its own.
const (
	AccessRequestPending   AccessRequestStatus = "pending"
	AccessRequestApproved  AccessRequestStatus = "approved"
	AccessRequestDenied    AccessRequestStatus = "denied"
	AccessRequestCancelled AccessRequestStatus = "cancelled"
)

// AccessRequestStatuses lists every status, in lifecycle order.
var AccessRequestStatuses = []AccessRequestStatus{
	AccessRequestPending, AccessRequestApproved, AccessRequestDenied, AccessRequestCancelled,
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
	// ExpiresAt is when the granted membership ends; set on approval.
	ExpiresAt *time.Time
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

// The bounds on how long a request may ask for.
const (
	MinAccessRequestDuration = 5 * time.Minute
	MaxAccessRequestDuration = 30 * 24 * time.Hour
	maxAccessRequestText     = 500
)

// Errors of the access request flow.
var (
	ErrAccessRequestNotFound       = errors.New("access request not found")
	ErrAccessRequestDecided        = errors.New("access request was already decided")
	ErrAccessRequestNoUser         = errors.New("an access request needs a user; the credential has none")
	ErrAccessRequestNotRequestable = errors.New("the group does not take access requests")
	ErrAccessRequestDuration       = errors.New("access request duration must be between 5 minutes and 30 days")
	ErrAccessRequestTextTooLong    = errors.New("access request text must be at most 500 characters")
	ErrAccessRequestNodeOwner      = errors.New("the machine must belong to the requester")
	ErrAccessRequestOwn            = errors.New("nobody decides their own access request")
	ErrAccessRequestPendingExists  = errors.New("a request for the same group and machine is already pending")
)

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
