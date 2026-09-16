package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// accessRequestRow is a row of access_requests; see schema.sql.
type accessRequestRow struct {
	ID              uint64 `sql:"primary_key"`
	UserID          uint64
	NodeID          *uint64
	GroupID         uint64
	Reason          string
	DurationSeconds int64
	Status          string
	DecidedBy       string
	Note            string
	CreatedAt       *time.Time
	DecidedAt       *time.Time
	ExpiresAt       *time.Time
	RevokedBy       string
	RevokedAt       *time.Time
	RevokeNote      string
}

type accessRequestRecord struct {
	AccessRequest accessRequestRow `alias:"access_requests"`
}

func (r accessRequestRow) request() types.AccessRequest {
	req := types.AccessRequest{
		ID:         types.AccessRequestID(r.ID),
		UserID:     types.UserID(r.UserID),
		GroupID:    types.GroupID(r.GroupID),
		Reason:     r.Reason,
		Duration:   time.Duration(r.DurationSeconds) * time.Second,
		Status:     types.AccessRequestStatus(r.Status),
		DecidedBy:  r.DecidedBy,
		Note:       r.Note,
		RevokedBy:  r.RevokedBy,
		RevokeNote: r.RevokeNote,
	}

	if r.NodeID != nil {
		id := types.NodeID(*r.NodeID)
		req.NodeID = &id
	}

	if r.CreatedAt != nil {
		req.CreatedAt = *r.CreatedAt
	}

	req.DecidedAt = utcPtr(r.DecidedAt)
	req.ExpiresAt = utcPtr(r.ExpiresAt)
	req.RevokedAt = utcPtr(r.RevokedAt)

	return req
}

// ListAccessRequests returns every request, newest first, or only one
// user's when a user is given.
func (hsdb *HSDatabase) ListAccessRequests(userID *types.UserID) ([]types.AccessRequest, error) {
	return Read(hsdb, func(rx *Tx) ([]types.AccessRequest, error) {
		stmt := jet.SELECT(table.AccessRequests.AllColumns).FROM(table.AccessRequests)

		if userID != nil {
			stmt = stmt.WHERE(table.AccessRequests.UserID.EQ(jet.Uint64(uint64(*userID))))
		}

		var records []accessRequestRecord

		err := rx.executor().query(stmt.ORDER_BY(table.AccessRequests.ID.DESC()), &records)
		if err != nil {
			return nil, fmt.Errorf("listing access requests: %w", err)
		}

		out := make([]types.AccessRequest, 0, len(records))
		for _, r := range records {
			out = append(out, r.AccessRequest.request())
		}

		return out, nil
	})
}

// GetAccessRequest returns one request.
func (hsdb *HSDatabase) GetAccessRequest(id types.AccessRequestID) (types.AccessRequest, error) {
	return Read(hsdb, func(rx *Tx) (types.AccessRequest, error) {
		return getAccessRequest(rx, id)
	})
}

func getAccessRequest(q Querier, id types.AccessRequestID) (types.AccessRequest, error) {
	var record accessRequestRecord

	err := q.executor().query(
		jet.SELECT(table.AccessRequests.AllColumns).FROM(table.AccessRequests).
			WHERE(table.AccessRequests.ID.EQ(jet.Uint64(uint64(id)))),
		&record,
	)
	if errors.Is(err, ErrNotFound) {
		return types.AccessRequest{}, types.ErrAccessRequestNotFound
	}

	if err != nil {
		return types.AccessRequest{}, fmt.Errorf("loading access request %d: %w", id, err)
	}

	return record.AccessRequest.request(), nil
}

// CreateAccessRequest stores a pending request. A pending request for
// the same user, group and machine is refused.
func (hsdb *HSDatabase) CreateAccessRequest(req types.AccessRequest) (types.AccessRequest, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessRequest, error) {
		ex := tx.executor()

		where := table.AccessRequests.UserID.EQ(jet.Uint64(uint64(req.UserID))).
			AND(table.AccessRequests.GroupID.EQ(jet.Uint64(uint64(req.GroupID)))).
			AND(table.AccessRequests.Status.EQ(jet.String(string(types.AccessRequestPending))))

		if req.NodeID == nil {
			where = where.AND(table.AccessRequests.NodeID.IS_NULL())
		} else {
			where = where.AND(table.AccessRequests.NodeID.EQ(jet.Uint64(req.NodeID.Uint64())))
		}

		var pending []accessRequestRecord

		err := ex.query(jet.SELECT(table.AccessRequests.ID).FROM(table.AccessRequests).WHERE(where), &pending)
		if err != nil {
			return types.AccessRequest{}, fmt.Errorf("checking pending access requests: %w", err)
		}

		if len(pending) > 0 {
			return types.AccessRequest{}, types.ErrAccessRequestPendingExists
		}

		now := time.Now().UTC()
		row := accessRequestRow{
			UserID:          uint64(req.UserID),
			GroupID:         uint64(req.GroupID),
			Reason:          req.Reason,
			DurationSeconds: int64(req.Duration / time.Second),
			Status:          string(types.AccessRequestPending),
			CreatedAt:       &now,
		}

		if req.NodeID != nil {
			id := req.NodeID.Uint64()
			row.NodeID = &id
		}

		var inserted idRow

		err = ex.query(
			table.AccessRequests.INSERT(table.AccessRequests.MutableColumns).MODEL(&row).
				RETURNING(table.AccessRequests.ID.AS("id_row.id")),
			&inserted,
		)
		if err != nil {
			return types.AccessRequest{}, fmt.Errorf("creating access request: %w", err)
		}

		return getAccessRequest(tx, types.AccessRequestID(inserted.ID))
	})
}

// AccessRequestDecision is the outcome written on a pending request.
type AccessRequestDecision struct {
	Status    types.AccessRequestStatus
	DecidedBy string
	Note      string
	// ExpiresAt is when the granted membership ends; only an approval
	// carries one.
	ExpiresAt *time.Time
}

// DecideAccessRequest records the outcome and, for an approval, grants
// the membership in the same transaction. A request already decided is
// refused.
func (hsdb *HSDatabase) DecideAccessRequest(
	id types.AccessRequestID, decision AccessRequestDecision,
) (types.AccessRequest, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessRequest, error) {
		req, err := getAccessRequest(tx, id)
		if err != nil {
			return types.AccessRequest{}, err
		}

		if !req.Pending() {
			return types.AccessRequest{}, types.ErrAccessRequestDecided
		}

		now := time.Now().UTC()

		_, err = tx.executor().exec(
			table.AccessRequests.UPDATE(
				table.AccessRequests.Status, table.AccessRequests.DecidedBy, table.AccessRequests.Note,
				table.AccessRequests.DecidedAt, table.AccessRequests.ExpiresAt,
			).SET(
				string(decision.Status), decision.DecidedBy, decision.Note, now, utcPtr(decision.ExpiresAt),
			).WHERE(table.AccessRequests.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return types.AccessRequest{}, fmt.Errorf("deciding access request %d: %w", id, err)
		}

		if decision.Status == types.AccessRequestApproved && decision.ExpiresAt != nil {
			err = grantRequest(tx, req, *decision.ExpiresAt)
			if err != nil {
				return types.AccessRequest{}, err
			}
		}

		return getAccessRequest(tx, id)
	})
}

// AccessRequestRevocation is who ended an approval early and why.
type AccessRequestRevocation struct {
	RevokedBy string
	Note      string
}

// RevokeAccessRequest ends an approval before its expiry: it drops the
// membership the approval granted and marks the request revoked, keeping
// the approval's own record. A request that is not in effect is refused.
//
// The membership only goes when it is the one this approval made: a
// temporary membership that ends no later than the request does. A
// permanent member, or one another approval extended further, keeps the
// membership it had before, because this approval did not create it.
func (hsdb *HSDatabase) RevokeAccessRequest(
	id types.AccessRequestID, revocation AccessRequestRevocation,
) (types.AccessRequest, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessRequest, error) {
		req, err := getAccessRequest(tx, id)
		if err != nil {
			return types.AccessRequest{}, err
		}

		if !req.Active(time.Now()) {
			return types.AccessRequest{}, types.ErrAccessRequestNotActive
		}

		err = dropGrantedMembership(tx, req)
		if err != nil {
			return types.AccessRequest{}, err
		}

		now := time.Now().UTC()

		_, err = tx.executor().exec(
			table.AccessRequests.UPDATE(
				table.AccessRequests.Status, table.AccessRequests.RevokedBy,
				table.AccessRequests.RevokedAt, table.AccessRequests.RevokeNote,
			).SET(
				string(types.AccessRequestRevoked), revocation.RevokedBy, now, revocation.Note,
			).WHERE(table.AccessRequests.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return types.AccessRequest{}, fmt.Errorf("revoking access request %d: %w", id, err)
		}

		err = touchGroup(tx, req.GroupID)
		if err != nil {
			return types.AccessRequest{}, err
		}

		return getAccessRequest(tx, id)
	})
}

// dropGrantedMembership removes the temporary membership the approval
// granted, if it is still the one the approval made.
func dropGrantedMembership(q Querier, req types.AccessRequest) error {
	until := jet.TimestampExp(timeArg(req.ExpiresAt.UTC()))

	if req.NodeID != nil {
		_, err := q.executor().exec(
			table.GroupNodes.DELETE().WHERE(
				table.GroupNodes.GroupID.EQ(jet.Uint64(uint64(req.GroupID))).
					AND(table.GroupNodes.NodeID.EQ(jet.Uint64(req.NodeID.Uint64()))).
					AND(table.GroupNodes.ExpiresAt.IS_NOT_NULL()).
					AND(table.GroupNodes.ExpiresAt.LT_EQ(until)),
			),
		)
		if err != nil {
			return fmt.Errorf("revoking node %d in group %d: %w", *req.NodeID, req.GroupID, err)
		}

		return nil
	}

	_, err := q.executor().exec(
		table.GroupUsers.DELETE().WHERE(
			table.GroupUsers.GroupID.EQ(jet.Uint64(uint64(req.GroupID))).
				AND(table.GroupUsers.UserID.EQ(jet.Uint64(uint64(req.UserID)))).
				AND(table.GroupUsers.ExpiresAt.IS_NOT_NULL()).
				AND(table.GroupUsers.ExpiresAt.LT_EQ(until)),
		),
	)
	if err != nil {
		return fmt.Errorf("revoking user %d in group %d: %w", req.UserID, req.GroupID, err)
	}

	return nil
}

// RevokeAccessRequestsForMember marks revoked every approval still in
// effect that granted the member its place in the group. It runs when an
// operator removes the membership by hand, so the request list and the
// group's members never disagree.
func revokeAccessRequestsForMember(
	q Querier, groupID types.GroupID, userID types.UserID, nodeID *types.NodeID, by string,
) error {
	now := time.Now().UTC()
	at := jet.TimestampExp(timeArg(now))

	where := table.AccessRequests.GroupID.EQ(jet.Uint64(uint64(groupID))).
		AND(table.AccessRequests.Status.EQ(jet.String(string(types.AccessRequestApproved)))).
		AND(table.AccessRequests.ExpiresAt.IS_NOT_NULL()).
		AND(table.AccessRequests.ExpiresAt.GT(at))

	if nodeID == nil {
		where = where.AND(table.AccessRequests.UserID.EQ(jet.Uint64(uint64(userID)))).
			AND(table.AccessRequests.NodeID.IS_NULL())
	} else {
		where = where.AND(table.AccessRequests.NodeID.EQ(jet.Uint64(nodeID.Uint64())))
	}

	_, err := q.executor().exec(
		table.AccessRequests.UPDATE(
			table.AccessRequests.Status, table.AccessRequests.RevokedBy,
			table.AccessRequests.RevokedAt, table.AccessRequests.RevokeNote,
		).SET(
			string(types.AccessRequestRevoked), by, now, "The membership was removed.",
		).WHERE(where),
	)
	if err != nil {
		return fmt.Errorf("revoking access requests for group %d: %w", groupID, err)
	}

	return nil
}

// grantRequest adds the membership an approval asks for.
func grantRequest(q Querier, req types.AccessRequest, expiresAt time.Time) error {
	var err error

	if req.NodeID != nil {
		err = GrantGroupNode(q, req.GroupID, *req.NodeID, expiresAt)
	} else {
		err = GrantGroupUser(q, req.GroupID, req.UserID, expiresAt)
	}

	if err != nil {
		return err
	}

	return touchGroup(q, req.GroupID)
}

// DeleteAccessRequest removes a request. An approval still in effect is
// refused: deleting the row would leave the membership behind with
// nothing to show it.
func (hsdb *HSDatabase) DeleteAccessRequest(id types.AccessRequestID) error {
	return hsdb.Write(func(tx *Tx) error {
		req, err := getAccessRequest(tx, id)
		if err != nil {
			return err
		}

		if req.Active(time.Now()) {
			return types.ErrAccessRequestActive
		}

		affected, err := tx.executor().exec(
			table.AccessRequests.DELETE().WHERE(table.AccessRequests.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting access request %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrAccessRequestNotFound
		}

		return nil
	})
}
