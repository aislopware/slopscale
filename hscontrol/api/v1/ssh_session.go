package apiv1

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"tailscale.com/types/views"
	"tailscale.com/util/dnsname"
)

func init() {
	registrations = append(registrations, registerSSHSession)
}

// sshSessionKeyTTL is how long the browser has to join the tailnet with
// the key; the node the key registers is ephemeral, so it disappears
// when the tab closes.
const sshSessionKeyTTL = 5 * time.Minute

// sshSessionHostname is the hostname the browser node registers under;
// the server disambiguates it as it does any hostname.
const sshSessionHostname = "browser"

// SSHSession is what the console needs to open an SSH terminal in the
// browser: a one-time key that joins an ephemeral node of the caller's
// own to the tailnet, and where the target is.
type SSHSession struct {
	// AuthKey joins the browser to the tailnet as an ephemeral, single-use
	// node owned by the caller; it is valid for five minutes.
	AuthKey    string    `doc:"One-time key for the browser's ephemeral node, valid five minutes." json:"authKey"`
	ControlURL string    `doc:"The control URL the browser connects to."                           json:"controlUrl"`
	Hostname   string    `doc:"The hostname the browser node registers under."                     json:"hostname"`
	ExpiresAt  time.Time `doc:"When the key stops working."                                        json:"expiresAt"`
	// Target is the machine to open a session to.
	Target SSHSessionTarget `json:"target"`
}

// SSHSessionTarget is the machine the session is opened to.
type SSHSessionTarget struct {
	NodeID    string   `format:"uint64"                                              json:"nodeId"`
	Name      string   `doc:"The node's given name, which the browser dials."        json:"name"`
	DNSName   string   `doc:"The node's MagicDNS name, empty without a base domain." json:"dnsName"`
	Addresses []string `json:"addresses"                                             nullable:"false"`
	Online    bool     `json:"online"`
	// SSHServer reports whether the client runs Tailscale SSH, which the
	// session needs on the target.
	SSHServer bool `doc:"true while the target runs Tailscale SSH (tailscale set --ssh)." json:"sshServer"`
	// Username suggests the login to use: the caller's login name on a
	// user-owned node, root on a tagged one.
	Username string `doc:"A suggested login name." json:"username"`
}

// CreateSSHSessionRequestBody names the target.
type CreateSSHSessionRequestBody struct {
	NodeID string `doc:"The node to open a session to." format:"uint64" json:"nodeId"`
}

type (
	createSSHSessionInput struct {
		Body CreateSSHSessionRequestBody
	}
	sshSessionOutput struct {
		Body SSHSession
	}
)

func registerSSHSession(api huma.API, b Backend) {
	huma.Register(api, audited(huma.Operation{
		OperationID: "createSSHSession",
		Method:      http.MethodPost,
		Path:        "/api/v1/ssh-session",
		Summary:     "Start a browser SSH session",
		Description: "Mints a one-time key for an ephemeral node owned by the caller, which the console's " +
			"in-browser client joins the tailnet with to open Tailscale SSH to the target. Whether the " +
			"session is allowed is the SSH policy's call, as for any other machine of the caller's; the " +
			"credential must belong to a user, and without the devices:core:read scope the target must " +
			"be a node of the caller's own or one their machines can already reach.",
		Tags:          []string{"Nodes"},
		Security:      bearerAuth,
		DefaultStatus: http.StatusCreated,
	}, "ssh.session.start", "node", ""), func(
		ctx context.Context, in *createSSHSessionInput,
	) (*sshSessionOutput, error) {
		p := caller(ctx)
		if !p.HasUser() {
			return nil, huma.Error403Forbidden("the credential has no user, so it cannot open a session of its own")
		}

		nodeID, err := parseNodeID(in.Body.NodeID)
		if err != nil {
			return nil, err
		}

		node, ok := b.State.GetNodeByID(nodeID)
		if !ok || !b.canOpenSSHSession(p, node) {
			return nil, huma.Error404NotFound(fmt.Sprintf("node %d not found", nodeID))
		}

		user, err := b.State.GetUserByID(p.UserID)
		if err != nil {
			return nil, mapError("starting ssh session", err)
		}

		expires := time.Now().Add(sshSessionKeyTTL)
		userID := p.UserID

		key, err := b.State.CreatePreAuthKeyFromSpec(types.PreAuthKeySpec{
			UserID:        &userID,
			Ephemeral:     true,
			Preauthorized: true,
			Expiration:    &expires,
		})
		if err != nil {
			return nil, mapError("starting ssh session", err)
		}

		audit.Target(ctx, "node", node.StringID(), node.GivenName())
		audit.Detail(ctx, "preAuthKeyId", formatID(key.ID))

		out := &sshSessionOutput{}
		out.Body = SSHSession{
			AuthKey:    key.Key,
			ControlURL: b.Cfg.ServerURL,
			Hostname:   sshSessionHostname,
			ExpiresAt:  expires,
			Target:     b.sshSessionTarget(node, user),
		}

		return out, nil
	})
}

// canOpenSSHSession reports whether the caller may learn about the target
// and dial it: a credential that reads devices sees every node, as it
// does through GET /api/v1/node/{id}; any other user-owned one only its
// own nodes and the ones the policy lets a machine of theirs see, so a
// member cannot enumerate the tailnet through this endpoint.
func (b Backend) canOpenSSHSession(p principal.Principal, target types.NodeView) bool {
	if p.Allows(scope.DevicesCoreRead) {
		return true
	}

	if !target.IsTagged() && target.UserID().Valid() && types.UserID(target.UserID().Get()) == p.UserID {
		return true
	}

	candidate := views.SliceOf([]types.NodeView{target})

	for _, own := range b.State.ListNodesByUser(p.UserID).All() {
		if own.ID() != target.ID() && own.IsAdmitted() && b.State.VisiblePeers(own, candidate).Len() > 0 {
			return true
		}
	}

	return false
}

// sshSessionTarget describes the node the browser dials.
func (b Backend) sshSessionTarget(node types.NodeView, user *types.User) SSHSessionTarget {
	target := SSHSessionTarget{
		NodeID:    node.StringID(),
		Name:      node.GivenName(),
		Addresses: nonNilStrings(node.IPsAsString()),
		Online:    node.IsOnline().Valid() && node.IsOnline().Get(),
		Username:  "root",
	}

	if b.Cfg != nil && b.Cfg.BaseDomain != "" {
		target.DNSName = strings.TrimSuffix(dnsname.SanitizeHostname(node.GivenName()), ".") + "." + b.Cfg.BaseDomain
	}

	if hi := node.Hostinfo(); hi.Valid() {
		target.SSHServer = hi.SSH_HostKeys().Len() > 0
	}

	if !node.IsTagged() && user != nil {
		if login, _, ok := strings.Cut(user.Username(), "@"); ok && login != "" {
			target.Username = login
		} else if user.Username() != "" {
			target.Username = user.Username()
		}
	}

	return target
}
