package state

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
)

// AccessRequestOptions is what a user may ask for: the requestable
// groups and the machines they own.
type AccessRequestOptions struct {
	Groups []types.AccessGroup
	Nodes  []types.NodeView
}

// AccessRequestOptionsFor lists the groups that take requests and, for
// a user, the machines the request can be for.
func (s *State) AccessRequestOptionsFor(userID types.UserID) AccessRequestOptions {
	var opts AccessRequestOptions

	for _, g := range s.AccessModel().Groups {
		if g.Requestable && !g.IsBuiltin() {
			opts.Groups = append(opts.Groups, g)
		}
	}

	if userID != 0 {
		opts.Nodes = s.ListNodesByUser(userID).AsSlice()
	}

	return opts
}

// ListAccessRequests returns every request, or one user's own.
func (s *State) ListAccessRequests(userID *types.UserID) ([]types.AccessRequest, error) {
	return s.db.ListAccessRequests(userID)
}

// GetAccessRequest returns one request.
func (s *State) GetAccessRequest(id types.AccessRequestID) (types.AccessRequest, error) {
	return s.db.GetAccessRequest(id)
}

// CreateAccessRequest files a user's ask to join a group for a while.
// The group must take requests and the machine, when named, must be the
// user's.
func (s *State) CreateAccessRequest(req types.AccessRequest) (types.AccessRequest, error) {
	if req.UserID == 0 {
		return types.AccessRequest{}, types.ErrAccessRequestNoUser
	}

	req.Reason = strings.TrimSpace(req.Reason)

	err := types.ValidateAccessRequestText(req.Reason)
	if err != nil {
		return types.AccessRequest{}, err
	}

	err = types.ValidateAccessRequestDuration(req.Duration)
	if err != nil {
		return types.AccessRequest{}, err
	}

	group, ok := s.AccessModel().Group(req.GroupID)
	if !ok {
		return types.AccessRequest{}, types.ErrGroupNotFound
	}

	if !group.Requestable || group.IsBuiltin() {
		return types.AccessRequest{}, types.ErrAccessRequestNotRequestable
	}

	if req.NodeID != nil {
		node, ok := s.GetNodeByID(*req.NodeID)
		if !ok {
			return types.AccessRequest{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, *req.NodeID)
		}

		if node.IsTagged() || !node.UserID().Valid() || types.UserID(node.UserID().Get()) != req.UserID {
			return types.AccessRequest{}, types.ErrAccessRequestNodeOwner
		}
	}

	created, err := s.db.CreateAccessRequest(req)
	if err != nil {
		return types.AccessRequest{}, err
	}

	log.Info().
		Uint64("request.id", uint64(created.ID)).
		Uint64("user.id", uint64(created.UserID)).
		Uint64("group.id", uint64(created.GroupID)).
		Msg("Access request filed")

	s.emitAccessRequest(types.EventAccessRequestCreated, created, "%s asked to join %s for %s.")

	return created, nil
}

// AccessDecision is what an approver says about a request.
type AccessDecision struct {
	// DeciderUserID is the approver when the credential has a user; a
	// user may not decide their own request.
	DeciderUserID types.UserID
	// DecidedBy names the approver for the record.
	DecidedBy string
	Note      string
	// Duration overrides what was asked for; zero keeps it.
	Duration time.Duration
}

// ApproveAccessRequest grants the membership for the duration and
// rebuilds the policy.
func (s *State) ApproveAccessRequest(
	id types.AccessRequestID, decision AccessDecision,
) (types.AccessRequest, change.Change, error) {
	req, err := s.checkDecision(id, decision)
	if err != nil {
		return types.AccessRequest{}, change.Change{}, err
	}

	duration := req.Duration
	if decision.Duration != 0 {
		duration = decision.Duration

		err = types.ValidateAccessRequestDuration(duration)
		if err != nil {
			return types.AccessRequest{}, change.Change{}, err
		}
	}

	expiresAt := time.Now().Add(duration).UTC()

	decided, err := s.db.DecideAccessRequest(id, hsdb.AccessRequestDecision{
		Status:    types.AccessRequestApproved,
		DecidedBy: decision.DecidedBy,
		Note:      strings.TrimSpace(decision.Note),
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		return types.AccessRequest{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.AccessRequest{}, change.Change{}, err
	}

	log.Info().
		Uint64("request.id", uint64(id)).
		Str("decided.by", decision.DecidedBy).
		Time("expires.at", expiresAt).
		Msg("Access request approved")

	s.emitAccessRequest(types.EventAccessRequestApproved, decided, "%s may join %s for %s.")

	return decided, c, nil
}

// DenyAccessRequest turns a request down.
func (s *State) DenyAccessRequest(id types.AccessRequestID, decision AccessDecision) (types.AccessRequest, error) {
	_, err := s.checkDecision(id, decision)
	if err != nil {
		return types.AccessRequest{}, err
	}

	decided, err := s.db.DecideAccessRequest(id, hsdb.AccessRequestDecision{
		Status:    types.AccessRequestDenied,
		DecidedBy: decision.DecidedBy,
		Note:      strings.TrimSpace(decision.Note),
	})
	if err != nil {
		return types.AccessRequest{}, err
	}

	log.Info().Uint64("request.id", uint64(id)).Str("decided.by", decision.DecidedBy).Msg("Access request denied")

	s.emitAccessRequest(types.EventAccessRequestDenied, decided, "%s was not let into %s (asked for %s).")

	return decided, nil
}

// checkDecision loads a pending request and refuses a self-decision.
func (s *State) checkDecision(id types.AccessRequestID, decision AccessDecision) (types.AccessRequest, error) {
	req, err := s.db.GetAccessRequest(id)
	if err != nil {
		return types.AccessRequest{}, err
	}

	if !req.Pending() {
		return types.AccessRequest{}, types.ErrAccessRequestDecided
	}

	if decision.DeciderUserID != 0 && decision.DeciderUserID == req.UserID {
		return types.AccessRequest{}, types.ErrAccessRequestOwn
	}

	err = types.ValidateAccessRequestText(decision.Note)
	if err != nil {
		return types.AccessRequest{}, err
	}

	return req, nil
}

// CancelAccessRequest withdraws a pending request.
func (s *State) CancelAccessRequest(id types.AccessRequestID, by string) (types.AccessRequest, error) {
	req, err := s.db.GetAccessRequest(id)
	if err != nil {
		return types.AccessRequest{}, err
	}

	if !req.Pending() {
		return types.AccessRequest{}, types.ErrAccessRequestDecided
	}

	return s.db.DecideAccessRequest(id, hsdb.AccessRequestDecision{
		Status: types.AccessRequestCancelled, DecidedBy: by,
	})
}

// DeleteAccessRequest removes a request from the record.
func (s *State) DeleteAccessRequest(id types.AccessRequestID) error {
	return s.db.DeleteAccessRequest(id)
}

// webhookAccessRequestData is the data an access request event carries.
type webhookAccessRequestData struct {
	RequestID string `json:"requestID"`
	User      string `json:"user"`
	UserID    string `json:"userID"`
	Group     string `json:"group"`
	GroupID   string `json:"groupID"`
	NodeID    string `json:"nodeID,omitempty"`
	Device    string `json:"deviceName,omitempty"`
	Duration  string `json:"duration"`
	Reason    string `json:"reason,omitempty"`
	Status    string `json:"status"`
	DecidedBy string `json:"decidedBy,omitempty"`
	Note      string `json:"note,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	URL       string `json:"url"`
}

// emitAccessRequest raises an event about a request; format takes the
// requester, the group and the duration.
func (s *State) emitAccessRequest(t types.WebhookEventType, req types.AccessRequest, format string) {
	if s == nil || s.webhooks == nil {
		return
	}

	data := webhookAccessRequestData{
		RequestID: req.ID.String(),
		UserID:    userIDString(req.UserID),
		GroupID:   req.GroupID.String(),
		Duration:  req.Duration.String(),
		Reason:    req.Reason,
		Status:    string(req.Status),
		DecidedBy: req.DecidedBy,
		Note:      req.Note,
		URL:       s.consoleURL("policy?tab=requests"),
	}

	data.User = "user " + userIDString(req.UserID)

	user, err := s.GetUserByID(req.UserID)
	if err == nil {
		data.User = userLabel(user)
	}

	if group, ok := s.AccessModel().Group(req.GroupID); ok {
		data.Group = group.Name
	} else {
		data.Group = "group " + req.GroupID.String()
	}

	if req.NodeID != nil {
		data.NodeID = req.NodeID.String()

		if node, ok := s.GetNodeByID(*req.NodeID); ok {
			data.Device = node.GivenName()
		}
	}

	if req.ExpiresAt != nil {
		data.ExpiresAt = req.ExpiresAt.Format(time.RFC3339)
	}

	s.emit(t, fmt.Sprintf(format, data.User, data.Group, req.Duration), data)
}

// userIDString renders a user id for event data.
func userIDString(id types.UserID) string {
	return strconv.FormatUint(uint64(id), 10)
}
