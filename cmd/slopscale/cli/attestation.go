package cli

import (
	"context"
	"fmt"
	"net/http"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
)

func init() {
	attestationCmd.PersistentFlags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkPersistentRequired(attestationCmd, "identifier")
	nodeCmd.AddCommand(attestationCmd)

	attestationCmd.AddCommand(attestationResetCmd)
}

var attestationCmd = &cobra.Command{
	Use:   "attestation",
	Short: "Manage what a node's hardware attestation key has proved",
	Long: `A client built with TPM support and started with tailscaled
--hardware-attestation signs every map request with a key that cannot leave
the machine's TPM. The server records what verified and offers it to the
policy as node:hardwareAttested. See docs/ref/device-trust.md.`,
}

var attestationResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Forget what a node's attestation key proved",
	Long: `Clears the stored record, so the next map request that carries a valid
signature starts it again. The machine is not touched and keeps its key; use
this after a TPM was cleared or a machine was reinstalled.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ResetNodeHardwareAttestationWithResponse(ctx, nodeIDFlag(cmd))
			if err != nil {
				return fmt.Errorf("resetting hardware attestation: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(
				cmd,
				map[string]string{colResult: "Hardware attestation reset"},
				"Hardware attestation reset",
			)
		},
	),
}
