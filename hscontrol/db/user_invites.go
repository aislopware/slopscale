package db

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

// inviteTokenBytes is the entropy of an invite token. Like a session
// token it is looked up by its SHA-256, so it needs no slow hash.
const inviteTokenBytes = 32

// NewInviteToken mints the random token an invite link carries and the
// hash the row stores. The token is shown once, when the invite is
// created or re-sent.
func NewInviteToken() (string, []byte, error) {
	raw := make([]byte, inviteTokenBytes)

	_, err := rand.Read(raw)
	if err != nil {
		return "", nil, fmt.Errorf("generating invite token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(raw)

	return token, HashInviteToken(token), nil
}

// HashInviteToken is how an invite token is stored and looked up.
func HashInviteToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))

	return sum[:]
}

// NormaliseInviteEmail is the form an invite email is stored and matched
// in: trimmed and lowercased, so an address invited as "Ada@Example.com"
// matches the claim "ada@example.com".
func NormaliseInviteEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// userInviteRow is a row of the user_invites table; see schema.sql.
type userInviteRow struct {
	ID             uint64 `sql:"primary_key"`
	TokenHash      []byte
	Email          string
	Role           string
	Groups         *string
	ExpiresAt      *time.Time
	CreatedAt      *time.Time
	CreatedBy      *uint64
	AcceptedAt     *time.Time
	AcceptedUserID *uint64
}

type userInviteRecord struct {
	Invite userInviteRow `alias:"user_invites"`
}

func (r userInviteRow) invite() (types.UserInvite, error) {
	i := types.UserInvite{
		ID:         types.UserInviteID(r.ID),
		Email:      r.Email,
		Role:       types.Role(r.Role),
		AcceptedAt: r.AcceptedAt,
	}

	if r.ExpiresAt != nil {
		i.ExpiresAt = *r.ExpiresAt
	}

	if r.CreatedAt != nil {
		i.CreatedAt = *r.CreatedAt
	}

	if r.CreatedBy != nil {
		i.CreatedBy = types.UserID(*r.CreatedBy)
	}

	if r.AcceptedUserID != nil {
		i.AcceptedUserID = types.UserID(*r.AcceptedUserID)
	}

	if r.Groups != nil && *r.Groups != "" {
		err := json.Unmarshal([]byte(*r.Groups), &i.GroupIDs)
		if err != nil {
			return types.UserInvite{}, fmt.Errorf("invite %d holds groups %q: %w", r.ID, *r.Groups, err)
		}
	}

	return i, nil
}

// CreateUserInvite stores an invite with the hash of its token.
func (hsdb *HSDatabase) CreateUserInvite(invite types.UserInvite, tokenHash []byte) (types.UserInvite, error) {
	groups, err := json.Marshal(invite.GroupIDs)
	if err != nil {
		return types.UserInvite{}, fmt.Errorf("encoding invite groups: %w", err)
	}

	now := time.Now().UTC()
	expires := invite.ExpiresAt.UTC()
	encoded := string(groups)

	row := userInviteRow{
		TokenHash: tokenHash,
		Email:     NormaliseInviteEmail(invite.Email),
		Role:      string(invite.Role),
		Groups:    &encoded,
		ExpiresAt: &expires,
		CreatedAt: &now,
	}

	if invite.CreatedBy != 0 {
		id := uint64(invite.CreatedBy)
		row.CreatedBy = &id
	}

	var inserted idRow

	err = hsdb.ex.query(
		table.UserInvites.INSERT(table.UserInvites.MutableColumns).MODEL(&row).
			RETURNING(table.UserInvites.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return types.UserInvite{}, fmt.Errorf("creating invite: %w", err)
	}

	row.ID = inserted.ID

	return row.invite()
}

// ListUserInvites returns every invite, pending and accepted, newest
// first.
func (hsdb *HSDatabase) ListUserInvites() ([]types.UserInvite, error) {
	return userInvites(hsdb, jet.Bool(true))
}

// GetUserInvite reads one invite.
func (hsdb *HSDatabase) GetUserInvite(id types.UserInviteID) (types.UserInvite, error) {
	return getUserInvite(hsdb, id)
}

func getUserInvite(q Querier, id types.UserInviteID) (types.UserInvite, error) {
	invites, err := userInvites(q, table.UserInvites.ID.EQ(jet.Uint64(uint64(id))))
	if err != nil {
		return types.UserInvite{}, err
	}

	if len(invites) == 0 {
		return types.UserInvite{}, types.ErrInviteNotFound
	}

	return invites[0], nil
}

// GetUserInviteByToken resolves the token from an invite link.
func (hsdb *HSDatabase) GetUserInviteByToken(token string) (types.UserInvite, error) {
	if token == "" {
		return types.UserInvite{}, types.ErrInviteNotFound
	}

	invites, err := userInvites(hsdb, table.UserInvites.TokenHash.EQ(jet.Blob(HashInviteToken(token))))
	if err != nil {
		return types.UserInvite{}, err
	}

	if len(invites) == 0 {
		return types.UserInvite{}, types.ErrInviteNotFound
	}

	return invites[0], nil
}

// GetPendingUserInviteByEmail returns the invite waiting for email that
// has not expired at now, or [types.ErrInviteNotFound]. The newest wins,
// which is the one a re-send would have produced.
func (hsdb *HSDatabase) GetPendingUserInviteByEmail(email string, now time.Time) (types.UserInvite, error) {
	normalised := NormaliseInviteEmail(email)
	if normalised == "" {
		return types.UserInvite{}, types.ErrInviteNotFound
	}

	invites, err := userInvites(hsdb,
		table.UserInvites.Email.EQ(jet.String(normalised)).
			AND(table.UserInvites.AcceptedAt.IS_NULL()).
			AND(table.UserInvites.ExpiresAt.GT(jet.TimestampExp(timeArg(now)))),
	)
	if err != nil {
		return types.UserInvite{}, err
	}

	if len(invites) == 0 {
		return types.UserInvite{}, types.ErrInviteNotFound
	}

	return invites[0], nil
}

// userInvites reads the invites matching where, newest first.
func userInvites(q Querier, where jet.BoolExpression) ([]types.UserInvite, error) {
	var records []userInviteRecord

	err := q.executor().query(
		jet.SELECT(table.UserInvites.AllColumns).FROM(table.UserInvites).
			WHERE(where).ORDER_BY(table.UserInvites.ID.DESC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("reading invites: %w", err)
	}

	out := make([]types.UserInvite, 0, len(records))

	for _, r := range records {
		invite, err := r.Invite.invite()
		if err != nil {
			return nil, err
		}

		out = append(out, invite)
	}

	return out, nil
}

// RotateUserInviteToken gives an invite a new token and expiry, which is
// what re-sending it means: the link in the old mail stops working.
func (hsdb *HSDatabase) RotateUserInviteToken(
	id types.UserInviteID,
	tokenHash []byte,
	expiresAt time.Time,
) (types.UserInvite, error) {
	return Write(hsdb, func(tx *Tx) (types.UserInvite, error) {
		affected, err := tx.executor().exec(
			table.UserInvites.UPDATE(table.UserInvites.TokenHash, table.UserInvites.ExpiresAt).
				SET(tokenHash, expiresAt.UTC()).
				WHERE(table.UserInvites.ID.EQ(jet.Uint64(uint64(id))).
					AND(table.UserInvites.AcceptedAt.IS_NULL())),
		)
		if err != nil {
			return types.UserInvite{}, fmt.Errorf("rotating invite %d: %w", id, err)
		}

		if affected == 0 {
			return types.UserInvite{}, types.ErrInviteNotFound
		}

		return getUserInvite(tx, id)
	})
}

// AcceptUserInvite marks the invite used by userID. It only touches an
// invite that is still pending, so two logins racing on one invite
// cannot both consume it.
func (hsdb *HSDatabase) AcceptUserInvite(
	id types.UserInviteID,
	userID types.UserID,
	at time.Time,
) error {
	affected, err := hsdb.ex.exec(
		table.UserInvites.UPDATE(table.UserInvites.AcceptedAt, table.UserInvites.AcceptedUserID).
			SET(at.UTC(), uint64(userID)).
			WHERE(table.UserInvites.ID.EQ(jet.Uint64(uint64(id))).
				AND(table.UserInvites.AcceptedAt.IS_NULL())),
	)
	if err != nil {
		return fmt.Errorf("accepting invite %d: %w", id, err)
	}

	if affected == 0 {
		return types.ErrInviteAccepted
	}

	return nil
}

// DeleteUserInvite revokes an invite.
func (hsdb *HSDatabase) DeleteUserInvite(id types.UserInviteID) error {
	affected, err := hsdb.ex.exec(
		table.UserInvites.DELETE().WHERE(table.UserInvites.ID.EQ(jet.Uint64(uint64(id)))),
	)
	if err != nil {
		return fmt.Errorf("deleting invite %d: %w", id, err)
	}

	if affected == 0 {
		return types.ErrInviteNotFound
	}

	return nil
}
