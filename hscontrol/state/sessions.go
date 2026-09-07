package state

import (
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
)

// CreateSession signs userID in to the admin console for
// [types.SessionLifetime] and returns the cookie token, which is shown to
// the browser once and stored only as a hash.
func (s *State) CreateSession(userID types.UserID) (string, *types.Session, error) {
	return s.db.CreateSession(userID, time.Now().Add(types.SessionLifetime))
}

// AuthenticateSession resolves a cookie token to its live session.
func (s *State) AuthenticateSession(token string) (*types.Session, error) {
	return s.db.AuthenticateSession(token)
}

// DeleteSession signs one session out.
func (s *State) DeleteSession(id uint64) error {
	return s.db.DeleteSession(id)
}

// DeleteUserSessions signs a user out of every browser.
func (s *State) DeleteUserSessions(userID types.UserID) (int64, error) {
	return s.db.DeleteUserSessions(userID)
}

// DeleteExpiredSessions removes sessions that expired before cutoff.
func (s *State) DeleteExpiredSessions(cutoff time.Time) (int64, error) {
	return s.db.DeleteExpiredSessions(cutoff)
}

// RecordAuditEvent appends one entry to the audit log and hands it to
// the log streams once it is stored, so a sink never sees an event the
// log does not hold.
func (s *State) RecordAuditEvent(e *types.AuditEvent) error {
	err := s.db.RecordAuditEvent(e)
	if err != nil {
		return err
	}

	if s.logStreams != nil {
		s.logStreams.Publish(*e)
	}

	return nil
}

// ListAuditEvents returns audit entries matching q, newest first.
func (s *State) ListAuditEvents(q types.AuditQuery) ([]types.AuditEvent, error) {
	return s.db.ListAuditEvents(q)
}

// DeleteAuditEventsBefore applies the audit retention.
func (s *State) DeleteAuditEventsBefore(cutoff time.Time) (int64, error) {
	return s.db.DeleteAuditEventsBefore(cutoff)
}
