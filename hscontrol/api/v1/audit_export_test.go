package apiv1

import (
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCSVSafe pins the cells a spreadsheet would evaluate. The prefix is an
// apostrophe, which Excel, LibreOffice and Sheets read as "this is text".
func TestCSVSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cell string
		want string
	}{
		{name: "empty", cell: "", want: ""},
		{name: "plain", cell: "node.create", want: "node.create"},
		{name: "equals", cell: `=HYPERLINK("http://evil","click")`, want: `'=HYPERLINK("http://evil","click")`},
		{name: "plus", cell: "+1+1", want: "'+1+1"},
		{name: "minus", cell: "-2+3", want: "'-2+3"},
		{name: "at", cell: "@SUM(A1)", want: "'@SUM(A1)"},
		{name: "tab", cell: "\t=1", want: "'\t=1"},
		{name: "carriage return", cell: "\r=1", want: "'\r=1"},
		{name: "inner trigger is harmless", cell: "a=b", want: "a=b"},
		{name: "json detail", cell: `{"tags":["tag:ci"]}`, want: `{"tags":["tag:ci"]}`},
		{name: "digits", cell: "200", want: "200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, csvSafe(tt.cell))
		})
	}
}

// TestAuditCSVRowDefusesFormulas proves the export escapes the fields whose
// content comes from whoever provoked the event, not just a helper in
// isolation: a node named "=cmd|..." must not run when the operator opens the
// export.
func TestAuditCSVRowDefusesFormulas(t *testing.T) {
	t.Parallel()

	row, err := auditCSVRow(&types.AuditEvent{
		ID:         7,
		CreatedAt:  time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC),
		Action:     "node.create",
		ActorKind:  types.ActorSystem,
		ActorName:  `=cmd|' /C calc'!A0`,
		TargetKind: "node",
		TargetID:   "3",
		TargetName: "-2+3",
		Outcome:    200,
		RemoteAddr: "@1.2.3.4",
		Detail:     map[string]any{"name": "@SUM(A1)"},
	})
	require.NoError(t, err)

	assert.Equal(t, `'=cmd|' /C calc'!A0`, row[5])
	assert.Equal(t, "'-2+3", row[8])
	assert.Equal(t, "'@1.2.3.4", row[10])
	assert.Equal(t, "7", row[0], "an id is not a formula")
	assert.False(t, strings.HasPrefix(row[11], "'"), "the detail cell starts with a brace")
}
