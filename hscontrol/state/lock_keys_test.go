package state

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"tailscale.com/types/key"
	"tailscale.com/types/tkatype"
)

// TestApplyLockKeysPinsSignedNodeKey pins that a re-registration cannot
// swap the lock key of a node that stays signed: the client reports the
// key itself, and a trusted key is what lets a node switch the lock off.
func TestApplyLockKeysPinsSignedNodeKey(t *testing.T) {
	t.Parallel()

	nodeKey := key.NewNode().Public()
	oldNL := key.NewTLPrivate().Public()
	newNL := key.NewTLPrivate().Public()
	sig := tkatype.MarshaledSignature("signed")

	tests := []struct {
		name     string
		node     types.Node
		newKey   key.NodePublic
		verified tkatype.MarshaledSignature
		want     key.TLPublic
	}{
		{
			name:   "signed node keeps its key",
			node:   types.Node{NodeKey: nodeKey, NLKey: oldNL, KeySignature: sig},
			newKey: nodeKey,
			want:   oldNL,
		},
		{
			name:   "unsigned node takes the reported key",
			node:   types.Node{NodeKey: nodeKey, NLKey: oldNL},
			newKey: nodeKey,
			want:   newNL,
		},
		{
			name:   "a new node key drops the signature and takes the key",
			node:   types.Node{NodeKey: nodeKey, NLKey: oldNL, KeySignature: sig},
			newKey: key.NewNode().Public(),
			want:   newNL,
		},
		{
			name:     "a freshly verified signature takes the key",
			node:     types.Node{NodeKey: nodeKey, NLKey: oldNL, KeySignature: sig},
			newKey:   nodeKey,
			verified: tkatype.MarshaledSignature("resigned"),
			want:     newNL,
		},
		{
			name:   "a signed node without a key takes the reported one",
			node:   types.Node{NodeKey: nodeKey, KeySignature: sig},
			newKey: nodeKey,
			want:   newNL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			node := tt.node
			applyLockKeys(&node, tt.newKey, newNL, tt.verified)
			assert.Equal(t, tt.want, node.NLKey)
		})
	}
}
