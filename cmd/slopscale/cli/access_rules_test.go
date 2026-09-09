package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func accessRuleFlags(cmd *cobra.Command) {
	cmd.Flags().Uint64P("identifier", "i", 0, "")
	cmd.Flags().StringP("name", "n", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().StringSliceP("src", "s", []string{}, "")
	cmd.Flags().StringSliceP("dst", "d", []string{}, "")
	cmd.Flags().StringP("protocol", "p", "all", "")
	cmd.Flags().String("ports", "", "")
	cmd.Flags().Bool("bidirectional", false, "")
	cmd.Flags().Bool("disabled", false, "")
}

func sampleRule() clientv1.AccessRule {
	return clientv1.AccessRule{
		Id:                  "5",
		Name:                "web-access",
		Description:         "Allow web traffic",
		Enabled:             true,
		Protocol:            "tcp",
		Ports:               "80,443",
		Bidirectional:       true,
		SourceGroupIds:      []string{"1", "2"},
		DestinationGroupIds: []string{"3"},
		CreatedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func oneWayDisabledRule() clientv1.AccessRule {
	return clientv1.AccessRule{
		Id:                  "6",
		Name:                "ssh-access",
		Description:         "SSH rule",
		Enabled:             false,
		Protocol:            "tcp",
		Ports:               "22",
		Bidirectional:       false,
		SourceGroupIds:      []string{"1"},
		DestinationGroupIds: []string{"4"},
		CreatedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestAccessRuleCommands(t *testing.T) {
	rule := sampleRule()
	disabledRule := oneWayDisabledRule()

	cases := []commandCase{
		{
			name: "access-rules list renders direction and enabled",
			src:  listAccessRulesCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/access-rule": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)
					writeJSON(t, w, clientv1.ListRulesOutputBody{Rules: []clientv1.AccessRule{rule, disabledRule}})
				},
			},
			wantIn: []string{"web-access", "ssh-access", "both", "one-way", "yes", "no"},
		},
		{
			name: "create posts protocol, ports, bidirectional and both id lists",
			src:  createAccessRuleCmd,
			flags: map[string]string{
				"name":          "web-access",
				"src":           "1,2",
				"dst":           "3",
				"protocol":      "tcp",
				"ports":         "80,443",
				"bidirectional": "true",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/access-rule": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)

					var body clientv1.CreateAccessRuleJSONRequestBody
					decodeBody(t, r, &body)
					assert.Equal(t, "web-access", body.Name)
					assert.Equal(t, "tcp", body.Protocol)
					require.NotNil(t, body.Ports)
					assert.Equal(t, "80,443", *body.Ports)
					require.NotNil(t, body.Bidirectional)
					assert.True(t, *body.Bidirectional)
					require.NotNil(t, body.SourceGroupIds)
					assert.Equal(t, []string{"1", "2"}, *body.SourceGroupIds)
					require.NotNil(t, body.DestinationGroupIds)
					assert.Equal(t, []string{"3"}, *body.DestinationGroupIds)
					require.NotNil(t, body.Enabled)
					assert.True(t, *body.Enabled)

					writeJSON(t, w, clientv1.RuleOutputBody{Rule: rule})
				},
			},
			want: "Access rule created\n",
		},
		{
			name:  "update replaces access rule",
			src:   updateAccessRuleCmd,
			flags: map[string]string{"identifier": "5", "name": "updated-web", "src": "1", "dst": "2"},
			routes: map[string]apiHandler{
				"PUT /api/v1/access-rule/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "5", r.PathValue("id"))

					var body clientv1.UpdateAccessRuleJSONRequestBody
					decodeBody(t, r, &body)
					assert.Equal(t, "updated-web", body.Name)
					assert.Equal(t, []string{"1"}, *body.SourceGroupIds)
					assert.Equal(t, []string{"2"}, *body.DestinationGroupIds)

					writeJSON(t, w, clientv1.RuleOutputBody{Rule: rule})
				},
			},
			want: "Access rule updated\n",
		},
		{
			name:  "enable PATCHes the switch",
			src:   enableAccessRuleCmd,
			flags: map[string]string{"identifier": "6"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/access-rule/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "6", r.PathValue("id"))

					var body clientv1.SetAccessRuleEnabledJSONRequestBody
					decodeBody(t, r, &body)
					assert.True(t, body.Enabled)

					enabledRule := disabledRule
					enabledRule.Enabled = true
					writeJSON(t, w, clientv1.RuleOutputBody{Rule: enabledRule})
				},
			},
			want: "Access rule enabled\n",
		},
		{
			name:  "disable PATCHes the switch",
			src:   disableAccessRuleCmd,
			flags: map[string]string{"identifier": "5"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/access-rule/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "5", r.PathValue("id"))

					var body clientv1.SetAccessRuleEnabledJSONRequestBody
					decodeBody(t, r, &body)
					assert.False(t, body.Enabled)

					disabledCopy := rule
					disabledCopy.Enabled = false
					writeJSON(t, w, clientv1.RuleOutputBody{Rule: disabledCopy})
				},
			},
			want: "Access rule disabled\n",
		},
		{
			name:  "delete",
			src:   deleteAccessRuleCmd,
			flags: map[string]string{"identifier": "5"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/access-rule/{id}": deleteOK("5"),
			},
			want: "Access rule deleted\n",
		},
	}

	runCommandCases(t, accessRuleFlags, cases)
}
