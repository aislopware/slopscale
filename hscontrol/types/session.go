package types

import "time"

// SessionCookieName is the cookie that carries a console session token.
const SessionCookieName = "headscale_session"

// SessionLifetime is how long a console sign-in lasts. The cookie and the
// row expire together; there is no sliding renewal, so a stolen cookie is
// bounded the same way as a fresh one.
const SessionLifetime = 7 * 24 * time.Hour

// Session is a browser sign-in to the admin console. The user signed in
// through the identity provider; the browser holds a random token in
// [SessionCookieName] and the server keeps only its hash. The session's
// authority is the user's role at request time, never at sign-in time.
type Session struct {
	ID         uint64
	UserID     UserID
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// Expired reports whether the session is past its expiry at now.
func (s *Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}
