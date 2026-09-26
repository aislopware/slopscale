// Command slopscale-flowd reports the traffic tailnet nodes send through a
// Linux gateway (exit node, subnet router or app connector) to the
// slopscale server that coordinates the tailnet. It reads the kernel's
// connection tracking table, the server names in TLS and QUIC handshakes
// and, when the server turns it on, runs a logging DNS resolver on the
// gateway's tailnet addresses. It authenticates with the gateway node's
// own identity token from the local tailscaled, so it needs no
// configuration beyond running next to tailscaled as root.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/aislopware/slopscale/flowd/agent"
	"tailscale.com/client/local"
)

// version is set by the release build (-X main.version=...).
var version = ""

const defaultSpoolBytes = 64 << 20

var errNotLinux = errors.New("slopscale-flowd runs on Linux gateways only")

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "slopscale-flowd:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		server     = flag.String("server", "", "slopscale server URL (default: the node's control URL)")
		socket     = flag.String("socket", "", "tailscaled socket (default: the platform's default)")
		iface      = flag.String("interface", "tailscale0", "the tailnet interface")
		stateDir   = flag.String("state-dir", "/var/lib/slopscale-flowd", "directory for the spool and state")
		spoolBytes = flag.Int64("spool-size", defaultSpoolBytes, "bytes of undelivered reports to keep")
		verbose    = flag.Bool("verbose", false, "log debug messages")
		showVer    = flag.Bool("version", false, "print the version and exit")
	)

	flag.Parse()

	if *showVer {
		fmt.Println(buildVersion())

		return nil
	}

	if runtime.GOOS != "linux" {
		return errNotLinux
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return agent.Run(ctx, agent.Options{
		Local:      &local.Client{Socket: *socket, UseSocketOnly: *socket != ""},
		Server:     *server,
		Interface:  *iface,
		StateDir:   *stateDir,
		SpoolBytes: *spoolBytes,
		Version:    buildVersion(),
		Logger:     logger,
	})
}

func buildVersion() string {
	if version != "" {
		return version
	}

	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}

	return "dev"
}
