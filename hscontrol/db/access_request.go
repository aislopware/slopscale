package db

import (
	"errors"
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
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
}

type accessRequestRecord struct {
	AccessRequest accessRequestRow `alias:"access_requests"`
}

func (r accessRequestRow) request() types.AccessRequest {
	req := types.AccessRequest{
		ID:        types.AccessRequestID(r.ID),
		UserID:    types.UserID(r.UserID),
		GroupID:   types.GroupID(r.GroupID),
		Reason:    r.Reason,
		Duration:  time.Duration(r.DurationSeconds) * time.Second,
		Status:    types.AccessRequestStatus(r.Status),
		DecidedBy: r.DecidedBy,
		Note:      r.Note,
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

// DeleteAccessRequest removes a request.
func (hsdb *HSDatabase) DeleteAccessRequest(id types.AccessRequestID) error {
	return hsdb.Write(func(tx *Tx) error {
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
