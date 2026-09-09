package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aislopware/slopscale/hscontrol/ingress"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/spf13/cobra"
)

var errIngressNeedsAuthKey = errors.New("an auth key is required: --auth-key or TS_AUTHKEY")

func init() {
	rootCmd.AddCommand(ingressCmd)

	ingressCmd.Flags().String("control-url", "", "URL of the slopscale server")
	ingressCmd.Flags().String("auth-key", "", "Pre-auth key with "+types.FunnelIngressTag+" (or TS_AUTHKEY)")
	ingressCmd.Flags().String("state-dir", "", "Directory for the node's keys")
	ingressCmd.Flags().String("hostname", types.FunnelIngressHostname, "Name of the ingress node")
	ingressCmd.Flags().StringSlice("listen", []string{":443", ":8443", ":10000"}, "Public addresses to accept TLS on")

	_ = ingressCmd.MarkFlagRequired("control-url")
	_ = ingressCmd.MarkFlagRequired("state-dir")
}

var ingressCmd = &cobra.Command{
	Use:   "ingress",
	Short: "Run a Funnel ingress node",
	Long: `Joins the tailnet as a Funnel ingress and delivers public TLS connections
to the machine named by the server name, over the machine's peer API. The
server runs one inside itself when funnel.enabled is set; this command runs
another on a machine with a public address, such as a small VPS in front of
a server behind NAT.

The auth key must carry ` + types.FunnelIngressTag + `, which is what the policy grants
the ingress capability to:

  slopscale preauthkeys create --tags ` + types.FunnelIngressTag + ` --preauthorized

Public DNS for the machines' MagicDNS names (or a wildcard under the base
domain) points at this machine.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		controlURL, _ := cmd.Flags().GetString("control-url")
		authKey, _ := cmd.Flags().GetString("auth-key")
		stateDir, _ := cmd.Flags().GetString("state-dir")
		hostname, _ := cmd.Flags().GetString("hostname")
		listen, _ := cmd.Flags().GetStringSlice("listen")

		if authKey == "" {
			authKey = os.Getenv("TS_AUTHKEY")
		}

		if authKey == "" {
			return errIngressNeedsAuthKey
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		err := ingress.Run(ctx, ingress.Node{
			ControlURL:  controlURL,
			AuthKey:     authKey,
			Hostname:    hostname,
			StateDir:    stateDir,
			ListenAddrs: listen,
		}, nil)
		if err != nil {
			return fmt.Errorf("running the ingress: %w", err)
		}

		return nil
	},
}
