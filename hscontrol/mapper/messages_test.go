package mapper

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

func TestDisplayMessages(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name     string
		node     types.Node
		approval bool
		suspend  bool
	}{
		{"approved", types.Node{ApprovedAt: &now}, false, false},
		{"waiting for approval", types.Node{}, true, false},
		{"suspended", types.Node{ApprovedAt: &now, SuspendedAt: &now}, false, true},
		{"suspended while waiting", types.Node{SuspendedAt: &now}, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			messages := displayMessages(tt.node.View(), "https://control.example.com/")

			// Both keys are always present so a change clears the old
			// message on the client.
			require.Contains(t, messages, approvalMessageID)
			require.Contains(t, messages, suspendedMessageID)

			approval := messages[approvalMessageID]
			if tt.approval {
				require.NotNil(t, approval)
				assert.Equal(t, tailcfg.SeverityMedium, approval.Severity)
				assert.True(t, approval.ImpactsConnectivity)
				require.NotNil(t, approval.PrimaryAction)
				assert.Equal(t, "https://control.example.com/admin/machines", approval.PrimaryAction.URL)
			} else {
				assert.Nil(t, approval)
			}

			if tt.suspend {
				assert.NotNil(t, messages[suspendedMessageID])
			} else {
				assert.Nil(t, messages[suspendedMessageID])
			}
		})
	}
}
