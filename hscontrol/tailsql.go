package hscontrol

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/tailscale/tailsql/server/tailsql"
	"tailscale.com/tsnet"
	"tailscale.com/tsweb"
	"tailscale.com/types/logger"
)

// ErrNoCertDomains is returned when no cert domains are available for HTTPS.
var ErrNoCertDomains = errors.New("no cert domains available for HTTPS")

// newTailSQLHTTPServer builds an [http.Server] for a tsnet-served endpoint
// with the same timeouts used elsewhere in hscontrol (see debug.go), instead
// of the timeout-less package-level http.Serve.
func newTailSQLHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: types.HTTPTimeout,
		ReadTimeout:       types.HTTPTimeout,
		WriteTimeout:      types.HTTPTimeout,
		IdleTimeout:       types.HTTPTimeout,
	}
}

func runTailSQLService(ctx context.Context, logf logger.Logf, stateDir, dbPath string) error {
	opts := tailsql.Options{
		Hostname: "tailsql-headscale",
		StateDir: stateDir,
		Sources: []tailsql.DBSpec{
			{
				Source: "headscale",
				Label:  "headscale - sqlite",
				Driver: "sqlite",
				URL:    fmt.Sprintf("file:%s?mode=ro", dbPath),
				Named: map[string]string{
					"schema": `select * from sqlite_schema`,
				},
			},
		},
	}

	tsNode := &tsnet.Server{
		Dir:      os.ExpandEnv(opts.StateDir),
		Hostname: opts.Hostname,
		Logf:     logger.Discard,
	}
	defer tsNode.Close()

	logf("Starting tailscale (hostname=%q)", opts.Hostname)

	lc, err := tsNode.LocalClient()
	if err != nil {
		return fmt.Errorf("connect local client: %w", err)
	}

	opts.LocalClient = lc // for authentication

	// Make sure the Tailscale node starts up. It might not, if it is a new node
	// and the user did not provide an auth key.
	st, err := tsNode.Up(ctx)
	if err != nil {
		return fmt.Errorf("starting tailscale: %w", err)
	}

	logf("tailscale started, node state %q", st.BackendState)

	// Reaching here, we have a running Tailscale node, now we can set up the
	// HTTP and/or HTTPS plumbing for TailSQL itself.
	tsql, err := tailsql.NewServer(opts)
	if err != nil {
		return fmt.Errorf("creating tailsql server: %w", err)
	}

	lst, err := tsNode.Listen("tcp", ":80")
	if err != nil {
		return fmt.Errorf("listen port 80: %w", err)
	}

	// redirectSrv, when non-nil, is the HTTP->HTTPS redirect server bound to
	// the port 80 listener; it is shut down alongside the main server below.
	var redirectSrv *http.Server

	if opts.ServeHTTPS {
		// When serving TLS, add a redirect from HTTP on port 80 to HTTPS on 443.
		certDomains := tsNode.CertDomains()
		if len(certDomains) == 0 {
			return ErrNoCertDomains
		}

		base := "https://" + certDomains[0]

		redirectSrv = newTailSQLHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := base + r.RequestURI
			//nolint:gosec // G710: target prefixed by trusted base URL
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
		}))

		redirectLst := lst

		go func() {
			serveErr := redirectSrv.Serve(redirectLst)
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				logf("tailsql redirect server: %v", serveErr)
			}
		}()

		// For the real service, start a separate listener.
		// Note: Replaces the port 80 listener.
		lst, err = tsNode.ListenTLS("tcp", ":443")
		if err != nil {
			return fmt.Errorf("listen TLS: %w", err)
		}

		logf("enabled serving via HTTPS")
	}

	mux := tsql.NewMux()
	tsweb.Debugger(mux)

	mainSrv := newTailSQLHTTPServer(mux)

	go func() {
		serveErr := mainSrv.Serve(lst)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logf("tailsql server: %v", serveErr)
		}
	}()

	logf("TailSQL started")
	<-ctx.Done()
	logf("TailSQL shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), types.HTTPShutdownTimeout)
	defer cancel()

	if redirectSrv != nil {
		redirectShutdownErr := redirectSrv.Shutdown(shutdownCtx)
		if redirectShutdownErr != nil {
			logf("shutting down tailsql redirect server: %v", redirectShutdownErr)
		}
	}

	mainShutdownErr := mainSrv.Shutdown(shutdownCtx)
	if mainShutdownErr != nil {
		logf("shutting down tailsql server: %v", mainShutdownErr)
	}

	err = tsNode.Close()
	if err != nil {
		return fmt.Errorf("closing tsnet node: %w", err)
	}

	return nil
}
