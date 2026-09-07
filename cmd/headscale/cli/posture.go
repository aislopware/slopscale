package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
)

func init() {
	postureCmd.PersistentFlags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkPersistentRequired(postureCmd, "identifier")
	nodeCmd.AddCommand(postureCmd)

	postureCmd.AddCommand(postureShowCmd)
	postureCmd.AddCommand(postureCollectCmd)

	postureSetCmd.Flags().String("expiry", "", "When the attribute disappears (RFC 3339 or a duration such as 8h)")
	postureSetCmd.Flags().String("comment", "", "Why the attribute is set")
	postureCmd.AddCommand(postureSetCmd)
	postureCmd.AddCommand(postureDeleteCmd)
}

var errAttributeArgs = errors.New("give the attribute as custom:key=value")

var postureCmd = &cobra.Command{
	Use:   "posture",
	Short: "Show and set what the policy can check about a node",
	Long: `Device posture is the attribute map the policy's postures evaluate:
node:os, node:osVersion, node:tsVersion and the like from what the client
reports, node:serialNumber once the server has asked the client for it,
and custom:... attributes set here. See docs/ref/device-trust.md.`,
}

var postureShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show a node's posture attributes",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			id := nodeIDFlag(cmd)

			resp, err := client.GetNodePostureWithResponse(ctx, id)
			if err != nil {
				return fmt.Errorf("getting node posture: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printPosture(cmd, resp.JSON200)
		},
	),
}

var postureCollectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Ask the node for its hardware serial numbers now",
	Long: `Sends the connected node a request for its serial numbers and waits for the
answer. Needs the posture identity setting (headscale settings set
--posture-identity=true) and a client with posture checking on
(tailscale set --posture-checking=true).`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			id := nodeIDFlag(cmd)

			resp, err := client.CollectNodePostureWithResponse(ctx, id)
			if err != nil {
				return fmt.Errorf("collecting node posture: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printPosture(cmd, resp.JSON200)
		},
	),
}

var postureSetCmd = &cobra.Command{
	Use:   "set custom:key=value",
	Short: "Set a custom posture attribute on the node",
	Long: `Sets a custom:... attribute. The value is a string, or a number or true/false
when it parses as one; quote it to keep a number as a string. --expiry removes
the attribute at that time, which is how a temporary grant is made:

  headscale nodes posture set -i 7 custom:oncall=true --expiry 8h`,
	Args: cobra.ExactArgs(1),
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
			key, raw, ok := strings.Cut(args[0], "=")
			if !ok || key == "" {
				return errAttributeArgs
			}

			body := clientv1.SetNodeAttributeJSONRequestBody{Value: attributeValue(raw)}

			if comment, _ := cmd.Flags().GetString("comment"); comment != "" {
				body.Comment = &comment
			}

			if expiry, _ := cmd.Flags().GetString("expiry"); expiry != "" {
				at, err := parseExpiry(expiry)
				if err != nil {
					return err
				}

				body.Expiry = &at
			}

			resp, err := client.SetNodeAttributeWithResponse(ctx, nodeIDFlag(cmd), key, body)
			if err != nil {
				return fmt.Errorf("setting node attribute: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printPosture(cmd, resp.JSON200)
		},
	),
}

var postureDeleteCmd = &cobra.Command{
	Use:   "delete custom:key",
	Short: "Delete a custom posture attribute",
	Args:  cobra.ExactArgs(1),
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
			resp, err := client.DeleteNodeAttributeWithResponse(ctx, nodeIDFlag(cmd), args[0])
			if err != nil {
				return fmt.Errorf("deleting node attribute: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printPosture(cmd, resp.JSON200)
		},
	),
}

func nodeIDFlag(cmd *cobra.Command) string {
	id, _ := cmd.Flags().GetUint64("identifier")

	return strconv.FormatUint(id, 10)
}

// attributeValue reads a value the way JSON would: a number or a boolean
// when it parses as one, a string otherwise; quotes keep a string.
func attributeValue(raw string) any {
	unquoted, err := strconv.Unquote(raw)
	if err == nil {
		return unquoted
	}

	var v any

	err = json.Unmarshal([]byte(raw), &v)
	if err == nil {
		switch v.(type) {
		case float64, bool:
			return v
		}
	}

	return raw
}

// parseExpiry accepts an RFC 3339 time or a duration from now.
func parseExpiry(s string) (time.Time, error) {
	d, err := time.ParseDuration(s)
	if err == nil {
		return time.Now().Add(d).UTC(), nil
	}

	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing --expiry: %w", err)
	}

	return at.UTC(), nil
}

func printPosture(cmd *cobra.Command, posture *clientv1.NodePosture) error {
	return printListOutput(cmd, posture, func() error {
		keys := make([]string, 0, len(posture.Attributes))
		for k := range posture.Attributes {
			keys = append(keys, k)
		}

		slices.Sort(keys)

		rows := make([][]string, 0, len(keys)+1)
		for _, k := range keys {
			rows = append(rows, []string{k, attributeString(posture.Attributes[k])})
		}

		switch {
		case posture.Identity == nil && !posture.IdentityCollectionOn:
			rows = append(rows, []string{"(identity)", "collection off; see headscale settings set --posture-identity"})
		case posture.Identity == nil:
			rows = append(rows, []string{"(identity)", "not collected yet"})
		case posture.Identity.Disabled:
			rows = append(rows, []string{
				"(identity)",
				"client has posture checking off, asked " + posture.Identity.CollectedAt.Format(
					HeadscaleDateTimeFormat,
				),
			})
		default:
			rows = append(
				rows,
				[]string{"(identity)", "collected " + posture.Identity.CollectedAt.Format(HeadscaleDateTimeFormat)},
			)
		}

		for _, c := range posture.Custom {
			if c.ExpiresAt != nil {
				rows = append(rows, []string{c.Key + " expires", c.ExpiresAt.Format(HeadscaleDateTimeFormat)})
			}
		}

		return renderTable([]string{"Attribute", "Value"}, rows)
	})
}

func attributeString(v any) string {
	switch x := v.(type) {
	case []any:
		parts := make([]string, len(x))
		for i, p := range x {
			parts[i] = fmt.Sprint(p)
		}

		return strings.Join(parts, ", ")
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
