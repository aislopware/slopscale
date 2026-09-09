package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerTailnetLock)
}

// TailnetLockKey is a signing key the tailnet lock authority trusts.
type TailnetLockKey struct {
	ID     string `doc:"The key's identifier, hex."                   json:"id"`
	Public string `doc:"The public key in tlpub: form."               json:"public"`
	Votes  uint   `doc:"The key's weight when the authority decides." json:"votes"`
}

// TailnetLock is the state of tailnet lock; see docs/ref/tailnet-lock.md.
// SupportDisablementAvailable is whether the API can switch the lock off,
// which needs the secret the initialising client minted for the operator.
// SignedNodeIDs are the nodes whose key carries a signature the authority
// accepts; UnsignedNodeIDs the rest, locked out while the lock is on.
//
//nolint:tagalign // aligned, the tags run past 120 columns
type TailnetLock struct {
	Enabled                     bool             `doc:"Whether the lock is on." json:"enabled"`
	Head                        string           `doc:"The latest update while on." json:"head"`
	Keys                        []TailnetLockKey `doc:"The trusted keys while on." json:"keys" nullable:"false"`
	SupportDisablementAvailable bool             `doc:"Whether the API can switch it off." json:"supportDisablementAvailable"` //nolint:lll // one tag
	SignedNodeIDs               []string         `doc:"Nodes with a signature." json:"signedNodeIds" nullable:"false"`
	UnsignedNodeIDs             []string         `doc:"Nodes without one." json:"unsignedNodeIds" nullable:"false"`
	EnabledAt                   *time.Time       `doc:"Last switched on." json:"enabledAt,omitempty"`
	DisabledAt                  *time.Time       `doc:"Last switched off." json:"disabledAt,omitempty"`
}

type tailnetLockOutput struct {
	Body TailnetLock
}

func tailnetLockFrom(status types.TailnetLockStatus) TailnetLock {
	out := TailnetLock{
		Enabled:                     status.Enabled,
		Head:                        status.Head,
		Keys:                        []TailnetLockKey{},
		SupportDisablementAvailable: status.SupportDisablementAvailable,
		SignedNodeIDs:               nodeIDStrings(status.Signed),
		UnsignedNodeIDs:             nodeIDStrings(status.Unsigned),
		EnabledAt:                   status.EnabledAt,
		DisabledAt:                  status.DisabledAt,
	}

	for _, k := range status.Keys {
		out.Keys = append(out.Keys, TailnetLockKey{ID: k.ID, Public: k.Public, Votes: k.Votes})
	}

	return out
}

func nodeIDStrings(ids []types.NodeID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strconv.FormatUint(id.Uint64(), 10))
	}

	return out
}

func registerTailnetLock(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getTailnetLock",
		Method:      http.MethodGet,
		Path:        "/api/v1/tailnet-lock",
		Summary:     "Get tailnet lock",
		Description: "Whether tailnet lock is on, the trusted signing keys and which nodes are signed. " +
			"The lock is switched on from a node with `tailscale lock init`.",
		Tags:     []string{tagSettings},
		Security: bearerAuth,
	}, scope.FeatureSettingsRead), func(_ context.Context, _ *struct{}) (*tailnetLockOutput, error) {
		return &tailnetLockOutput{Body: tailnetLockFrom(b.State.TailnetLock())}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "disableTailnetLock",
		Method:      http.MethodPost,
		Path:        "/api/v1/tailnet-lock/disable",
		Summary:     "Disable tailnet lock",
		Description: "Switches the lock off with the disablement secret the initialising client minted " +
			"for the operator (`tailscale lock init --gen-disablement-for-support`). Every node " +
			"drops its lock state and its node key signature. Refused when no such secret was recorded; " +
			"`tailscale lock disable <secret>` on a node works with any disablement secret.",
		Tags:     []string{tagSettings},
		Security: bearerAuth,
	}, scope.FeatureSettings), "tailnet_lock.disable", "", ""), func(
		_ context.Context, _ *struct{},
	) (*tailnetLockOutput, error) {
		c, err := b.State.DisableTailnetLock()
		if err != nil {
			return nil, mapError("disabling tailnet lock", err)
		}

		b.Change(c)

		return &tailnetLockOutput{Body: tailnetLockFrom(b.State.TailnetLock())}, nil
	})
}
