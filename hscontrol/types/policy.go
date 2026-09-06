package types

import (
	"errors"
	"time"
)

var (
	ErrPolicyNotFound         = errors.New("acl policy not found")
	ErrPolicyUpdateIsDisabled = errors.New("update is disabled for modes other than 'database'")
)

// Policy represents a policy in the database.
type Policy struct {
	ID        uint
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	// Data contains the policy in HuJSON format.
	Data string
}
