package db

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// sessionTokenBytes is the entropy of a console session token. The token
// is looked up by its SHA-256, which is enough for a 256-bit random value;
// unlike a password it cannot be guessed, so no slow hash is needed.
const sessionTokenBytes = 32

// sessionSeenGranularity bounds how often a session's last_seen_at is
// written: a console polls the API constantly, and the column is for
// showing an operator "last active", not for auditing every request.
const sessionSeenGranularity = time.Minute

var (
	ErrSessionNotFound = fmt.Errorf("session not found: %w", ErrNotFound)
	ErrSessionExpired  = errors.New("session expired")
)

// sessionRow is a row of the sessions table.
type sessionRow struct {
	ID         uint64 `sql:"primary_key"`
	TokenHash  []byte
	UserID     uint64
	CreatedAt  *time.Time
	ExpiresAt  *time.Time
	LastSeenAt *time.Time
	RemoteAddr *string
	UserAgent  *string
}

type sessionRecord struct {
	Session sessionRow `alias:"sessions"`
}

func (r *sessionRow) session() *types.Session {
	s := &types.Session{
		ID:     r.ID,
		UserID: types.UserID(r.UserID),
	}

	if r.RemoteAddr != nil {
		s.RemoteAddr = *r.RemoteAddr
	}

	if r.UserAgent != nil {
		s.UserAgent = *r.UserAgent
	}

	if r.CreatedAt != nil {
		s.CreatedAt = *r.CreatedAt
	}

	if r.ExpiresAt != nil {
		s.ExpiresAt = *r.ExpiresAt
	}

	if r.LastSeenAt != nil {
		s.LastSeenAt = *r.LastSeenAt
	}

	return s
}

func hashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))

	return sum[:]
}

// SessionClient is what the browser opening a session looked like: the
// address the request came from once the trusted proxies were resolved,
// and its User-Agent header.
type SessionClient struct {
	RemoteAddr string
	UserAgent  string
}

// CreateSession opens a console session for userID that lasts until
// expiresAt and returns the token the browser must present. The token is
// returned once; only its hash is stored.
func (hsdb *HSDatabase) CreateSession(
	userID types.UserID,
	expiresAt time.Time,
	client SessionClient,
) (string, *types.Session, error) {
	raw := make([]byte, sessionTokenBytes)

	_, err := rand.Read(raw)
	if err != nil {
		return "", nil, fmt.Errorf("generating session token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UTC()
	expiresAt = expiresAt.UTC()

	row := sessionRow{
		TokenHash:  hashSessionToken(token),
		UserID:     uint64(userID),
		CreatedAt:  &now,
		ExpiresAt:  &expiresAt,
		LastSeenAt: &now,
		RemoteAddr: optionalText(client.RemoteAddr),
		UserAgent:  optionalText(sessionUserAgent(client.UserAgent)),
	}

	var inserted idRow

	err = hsdb.ex.query(
		table.Sessions.INSERT(table.Sessions.MutableColumns).MODEL(&row).RETURNING(table.Sessions.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return "", nil, fmt.Errorf("saving session: %w", err)
	}

	row.ID = inserted.ID

	return token, row.session(), nil
}

// AuthenticateSession resolves a presented token to its live session,
// touching last_seen_at at most once a minute. A missing or expired
// session is an error; the caller treats both as "sign in again".
func (hsdb *HSDatabase) AuthenticateSession(token string) (*types.Session, error) {
	if token == "" {
		return nil, ErrSessionNotFound
	}

	var record sessionRecord

	err := hsdb.ex.query(
		jet.SELECT(table.Sessions.AllColumns).FROM(table.Sessions).
			WHERE(table.Sessions.TokenHash.EQ(jet.Blob(hashSessionToken(token)))).LIMIT(1),
		&record,
	)
	if err != nil {
		return nil, ErrSessionNotFound
	}

	session := record.Session.session()
	now := time.Now().UTC()

	if session.Expired(now) {
		return nil, ErrSessionExpired
	}

	if now.Sub(session.LastSeenAt) >= sessionSeenGranularity {
		_, err = hsdb.ex.exec(
			table.Sessions.UPDATE(table.Sessions.LastSeenAt).SET(jet.TimestampExp(timeArg(now))).
				WHERE(table.Sessions.ID.EQ(jet.Uint64(session.ID))),
		)
		if err != nil {
			return nil, fmt.Errorf("touching session: %w", err)
		}

		session.LastSeenAt = now
	}

	return session, nil
}

// maxUserAgentLength bounds what is stored of a User-Agent header: the
// column is for showing an operator which browser signed in, and the
// header is attacker-controlled.
const maxUserAgentLength = 512

// sessionUserAgent cuts a User-Agent header to what is worth storing.
func sessionUserAgent(agent string) string {
	if len(agent) > maxUserAgentLength {
		return agent[:maxUserAgentLength]
	}

	return agent
}

// optionalText keeps an empty string out of the database, so a column
// that was never filled reads back as NULL rather than "".
func optionalText(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// GetSession reads one session, whether or not it has expired.
func (hsdb *HSDatabase) GetSession(id uint64) (*types.Session, error) {
	var record sessionRecord

	err := hsdb.ex.query(
		jet.SELECT(table.Sessions.AllColumns).FROM(table.Sessions).
			WHERE(table.Sessions.ID.EQ(jet.Uint64(id))).LIMIT(1),
		&record,
	)
	if err != nil {
		return nil, ErrSessionNotFound
	}

	return record.Session.session(), nil
}

// ListSessions returns the sessions that have not expired at now, newest
// first. A non-zero userID narrows the list to that user's sessions.
func (hsdb *HSDatabase) ListSessions(userID types.UserID, now time.Time) ([]types.Session, error) {
	where := table.Sessions.ExpiresAt.GT(jet.TimestampExp(timeArg(now)))
	if userID != 0 {
		where = where.AND(table.Sessions.UserID.EQ(jet.Uint64(uint64(userID))))
	}

	var records []sessionRecord

	err := hsdb.ex.query(
		jet.SELECT(table.Sessions.AllColumns).FROM(table.Sessions).
			WHERE(where).ORDER_BY(table.Sessions.CreatedAt.DESC(), table.Sessions.ID.DESC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	out := make([]types.Session, 0, len(records))
	for i := range records {
		out = append(out, *records[i].Session.session())
	}

	return out, nil
}

// DeleteSession ends one session; a session that is already gone is not
// an error, so signing out twice is harmless.
func (hsdb *HSDatabase) DeleteSession(id uint64) error {
	_, err := hsdb.ex.exec(table.Sessions.DELETE().WHERE(table.Sessions.ID.EQ(jet.Uint64(id))))
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	return nil
}

// DeleteUserSessions signs a user out everywhere, returning how many
// sessions ended.
func (hsdb *HSDatabase) DeleteUserSessions(userID types.UserID) (int64, error) {
	return hsdb.ex.exec(table.Sessions.DELETE().WHERE(table.Sessions.UserID.EQ(jet.Uint64(uint64(userID)))))
}

// DeleteExpiredSessions removes sessions that expired before cutoff.
// Authentication already rejects them; this keeps the table bounded.
func (hsdb *HSDatabase) DeleteExpiredSessions(cutoff time.Time) (int64, error) {
	return hsdb.ex.exec(
		table.Sessions.DELETE().WHERE(table.Sessions.ExpiresAt.LT(jet.TimestampExp(timeArg(cutoff)))),
	)
}
