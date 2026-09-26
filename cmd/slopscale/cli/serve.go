package cli

import (
	"errors"
	"fmt"
	"net/http"
	"syscall"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/spf13/cobra"
	"github.com/tailscale/squibble"
)

func init() {
	rootCmd.AddCommand(serveCmd)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the server",
	RunE: func(_ *cobra.Command, _ []string) error {
		app, err := newSlopscaleServerWithConfig()
		if err != nil {
			if squibbleErr, ok := errors.AsType[squibble.ValidationError](err); ok {
				fmt.Printf("SQLite schema failed to validate:\n")
				fmt.Println(squibbleErr.Diff)
			}

			return fmt.Errorf("initializing: %w", err)
		}

		err = app.Serve()
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return classifyServeError(err)
	},
}

// classifyServeError adds a hint for the operator to a listener that could
// not bind. The chain stays intact, so errors.Is and errors.As still reach
// the [types.ListenerBindError] and the errno.
func classifyServeError(err error) error {
	bindErr, ok := errors.AsType[*types.ListenerBindError](err)
	if !ok {
		return err
	}

	switch {
	case errors.Is(err, syscall.EADDRINUSE):
		ssFlags := "-tlnp"
		if bindErr.Network == "udp" {
			ssFlags = "-ulnp"
		}

		port, portErr := types.PortFromAddr(bindErr.Addr)
		if portErr != nil {
			return fmt.Errorf(
				"%w\n\nHint: another socket on this host is bound to the same address. Find it with: sudo ss %s",
				err, ssFlags)
		}

		return fmt.Errorf(
			"%w\n\nHint: another socket on this host is bound to the same address. "+
				"Find it with: sudo ss %s 'sport = :%d'",
			err, ssFlags, port)

	case errors.Is(err, syscall.EACCES):
		return fmt.Errorf(
			"%w\n\nHint: binding a privileged port (below 1024) needs root or CAP_NET_BIND_SERVICE. "+
				"The packaged systemd unit grants the capability; when running by hand, use sudo or "+
				"`setcap cap_net_bind_service=+ep $(command -v slopscale)`",
			err)
	}

	return err
}
