package hscontrol

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/dnsprovider"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util/zlog"
	"github.com/caddyserver/certmagic"
	"github.com/mholt/acmez/v3/acme"
	"github.com/rs/zerolog/log"
)

var (
	errUnsupportedLetsEncryptChallengeType = errors.New(
		"unknown value for Lets Encrypt challenge type",
	)
	errAutocertNoKey  = errors.New("no private key")
	errAutocertNoCert = errors.New("no certificate")
)

// tlsBundle is the TLS configuration of the main listener, nil without
// TLS, and with the HTTP-01 challenge the server that answers it on a
// listener already bound, so a bind failure stops the start like any
// other listener's.
type tlsBundle struct {
	config       *tls.Config
	acmeServer   *http.Server
	acmeListener net.Listener

	// manage starts obtaining and renewing the certificate. It must run
	// after the main listener is bound: certmagic binds a challenge port
	// itself when nothing serves it yet. Nil without ACME.
	manage func(context.Context) error
	// stop ends certificate maintenance. Nil without ACME.
	stop func()
}

// serveACME answers the HTTP-01 challenge until [tlsBundle.shutdownACME];
// without the challenge it returns at once.
func (s *tlsBundle) serveACME() error {
	if s.acmeServer == nil {
		return nil
	}

	log.Info().Msgf("listening and serving ACME HTTP-01 challenge on: %s", s.acmeListener.Addr())

	err := s.acmeServer.Serve(s.acmeListener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving ACME HTTP-01 challenge: %w", err)
	}

	return nil
}

// shutdownACME stops the HTTP-01 challenge server and certificate
// maintenance.
func (s *tlsBundle) shutdownACME(ctx context.Context) {
	if s.stop != nil {
		s.stop()
	}

	if s.acmeServer == nil {
		return
	}

	log.Info().Msg("shutting down ACME HTTP-01 challenge server")

	err := s.acmeServer.Shutdown(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to shutdown ACME HTTP-01 challenge server")
	}
}

// acmeTrustedRoots verifies the ACME directory; nil means the system
// roots. Tests point it at their CA.
var acmeTrustedRoots *x509.CertPool

func (h *Slopscale) getTLSSettings(ctx context.Context) (*tlsBundle, error) {
	tlsEnabled := h.cfg.TLS.ACME.Hostname != "" || h.cfg.TLS.CertPath != ""
	if tlsEnabled && !strings.HasPrefix(h.cfg.ServerURL, "https://") {
		log.Warn().Msg("listening with TLS but ServerURL does not start with https://")
	} else if !tlsEnabled && !strings.HasPrefix(h.cfg.ServerURL, "http://") {
		log.Warn().Msg("listening without TLS but ServerURL does not start with http://")
	}

	if h.cfg.TLS.ACME.Hostname != "" {
		return h.acmeTLSSettings(ctx)
	}

	if h.cfg.TLS.CertPath == "" {
		return &tlsBundle{}, nil
	}

	cert, err := tls.LoadX509KeyPair(h.cfg.TLS.CertPath, h.cfg.TLS.KeyPath)
	if err != nil {
		return nil, fmt.Errorf(
			"loading TLS key pair (tls.cert_path=%q, tls.key_path=%q): %w",
			h.cfg.TLS.CertPath,
			h.cfg.TLS.KeyPath,
			err,
		)
	}

	return &tlsBundle{
		config: &tls.Config{
			NextProtos:   []string{"http/1.1"},
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}, nil
}

// acmeTLSSettings gets the server's own certificate with certmagic,
// which obtains it at start and keeps it renewed. DNS-01 publishes the
// challenge through the same DNS provider as machines' certificates.
func (h *Slopscale) acmeTLSSettings(ctx context.Context) (*tlsBundle, error) {
	acmeCfg := h.cfg.TLS.ACME
	logger := zlog.Zap(log.Logger).Named("acme")

	// An empty CA means Let's Encrypt, and certmagic then retries failures
	// against Let's Encrypt staging to spare the production rate limits.
	issuer := certmagic.ACMEIssuer{
		CA:           acmeCfg.URL,
		Email:        acmeCfg.Email,
		Agreed:       true,
		TrustedRoots: acmeTrustedRoots,
		Logger:       logger,
	}

	var err error

	switch acmeCfg.ChallengeType {
	case types.DNS01ChallengeType:
		ttl := h.cfg.HTTPSCerts.TTL
		if ttl <= 0 {
			ttl = dnsprovider.DefaultChallengeTTL
		}

		issuer.DNS01Solver = dns01Solver{
			DNS01Solver: &certmagic.DNS01Solver{
				DNSProvider: h.cfg.HTTPSCerts.Provider,
				TTL:         ttl,
				Resolvers:   h.cfg.HTTPSCerts.Resolvers,
				Logger:      logger,
			},
			wait: dnsprovider.AwaitTXT(h.cfg.HTTPSCerts.Resolvers, h.cfg.HTTPSCerts.PropagationTimeout),
		}

	case types.TLSALPN01ChallengeType:
		// Validated on the main listener, which must be reachable on port 443.
		issuer.DisableHTTPChallenge = true
		issuer.ListenHost, _, _ = net.SplitHostPort(h.cfg.Addr)

		issuer.AltTLSALPNPort, err = types.PortFromAddr(h.cfg.Addr)
		if err != nil {
			return nil, fmt.Errorf("parsing listen_addr: %w", err)
		}

	case types.HTTP01ChallengeType:
		// TLS-ALPN-01 stays on, as with autocert: setups that only open
		// port 443 have always been validated that way.
		issuer.AltTLSALPNPort, err = types.PortFromAddr(h.cfg.Addr)
		if err != nil {
			return nil, fmt.Errorf("parsing listen_addr: %w", err)
		}

		issuer.ListenHost, _, _ = net.SplitHostPort(acmeCfg.HTTPListen)

		issuer.AltHTTPPort, err = types.PortFromAddr(acmeCfg.HTTPListen)
		if err != nil {
			return nil, fmt.Errorf("parsing tls.acme.http_listen: %w", err)
		}

	default:
		return nil, errUnsupportedLetsEncryptChallengeType
	}

	var magic *certmagic.Config

	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return magic, nil },
		Logger:           logger,
	})
	storage := &certmagic.FileStorage{Path: acmeCfg.CacheDir}
	magic = certmagic.New(cache, certmagic.Config{
		Storage: storage,
		Logger:  logger,
	})

	acmeIssuer := certmagic.NewACMEIssuer(magic, issuer)
	magic.Issuers = []certmagic.Issuer{acmeIssuer}

	// A server upgraded from autocert keeps the certificate it has
	// instead of ordering a new one on its first start.
	importAutocertCert(ctx, storage, acmeIssuer.IssuerKey(), acmeCfg.CacheDir, acmeCfg.Hostname)

	bundle := &tlsBundle{
		manage: func(ctx context.Context) error { return magic.ManageAsync(ctx, []string{acmeCfg.Hostname}) },
		stop:   cache.Stop,
	}

	bundle.config = magic.TLSConfig()
	bundle.config.NextProtos = append([]string{"h2", "http/1.1"}, bundle.config.NextProtos...)
	bundle.config.MinVersion = tls.VersionTLS12

	if acmeCfg.ChallengeType == types.HTTP01ChallengeType {
		bundle.acmeServer = &http.Server{
			Addr:        acmeCfg.HTTPListen,
			Handler:     acmeIssuer.HTTPChallengeHandler(http.HandlerFunc(h.redirect)),
			ReadTimeout: types.HTTPTimeout,
		}

		bundle.acmeListener, err = new(net.ListenConfig).Listen(ctx, "tcp", acmeCfg.HTTPListen)
		if err != nil {
			cache.Stop()

			return nil, &types.ListenerBindError{
				Listener:  "ACME HTTP-01 challenge",
				ConfigKey: "tls.acme.http_listen",
				Network:   "tcp",
				Addr:      acmeCfg.HTTPListen,
				Err:       err,
			}
		}
	}

	return bundle, nil
}

// dns01Solver checks propagation as for machines' certificates instead
// of the way certmagic does: certmagic accepts the first nameserver
// serving the value and follows only the first CNAME of a delegated
// challenge.
type dns01Solver struct {
	*certmagic.DNS01Solver

	wait func(ctx context.Context, fqdn, value string) error
}

// Wait runs before the CA is asked to validate. When it fails, the ACME
// client cleans the challenge up.
func (s dns01Solver) Wait(ctx context.Context, ch acme.Challenge) error {
	return s.wait(ctx, ch.DNS01TXTRecordName()+".", ch.DNS01KeyAuthorization())
}

// importAutocertCert copies the certificate autocert left in cacheDir
// for hostname into certmagic's storage, unless storage has one
// already. autocert kept the key and chain in one PEM file named after
// the host, "+rsa" appended for an RSA key. A file that is missing,
// unreadable, expired or for another name is skipped: certmagic then
// orders a fresh certificate, as it would have anyway.
func importAutocertCert(ctx context.Context, storage certmagic.Storage, issuerKey, cacheDir, hostname string) {
	certKey := certmagic.StorageKeys.SiteCert(issuerKey, hostname)
	if storage.Exists(ctx, certKey) {
		return
	}

	for _, name := range []string{hostname, hostname + "+rsa"} {
		path := filepath.Join(cacheDir, name)

		keyPEM, certPEM, err := readAutocertCert(path, hostname)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("not importing the autocert certificate")

			continue
		}

		err = storeImportedCert(ctx, storage, issuerKey, hostname, keyPEM, certPEM)
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("importing the autocert certificate")

			return
		}

		log.Info().Str("path", path).Msg("imported the certificate autocert obtained")

		return
	}
}

// readAutocertCert reads an autocert cache file and returns its key and
// chain as separate PEM, after checking they make a pair valid for
// hostname now.
func readAutocertCert(path, hostname string) ([]byte, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading autocert cache: %w", err)
	}

	var keyPEM, certPEM []byte

	for {
		var block *pem.Block

		block, data = pem.Decode(data)
		if block == nil {
			break
		}

		switch {
		case block.Type == "CERTIFICATE":
			certPEM = append(certPEM, pem.EncodeToMemory(block)...)
		case strings.HasSuffix(block.Type, "PRIVATE KEY") && keyPEM == nil:
			keyPEM = pem.EncodeToMemory(block)
		}
	}

	if keyPEM == nil {
		return nil, nil, errAutocertNoKey
	}

	if certPEM == nil {
		return nil, nil, errAutocertNoCert
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing autocert certificate: %w", err)
	}

	if time.Now().After(pair.Leaf.NotAfter) {
		return nil, nil, fmt.Errorf("autocert certificate expired at %s: %w", pair.Leaf.NotAfter, errAutocertNoCert)
	}

	err = pair.Leaf.VerifyHostname(hostname)
	if err != nil {
		return nil, nil, fmt.Errorf("autocert certificate: %w", err)
	}

	return keyPEM, certPEM, nil
}

// storeImportedCert writes the three items certmagic loads a site's
// certificate from: key, chain and metadata naming its SANs.
func storeImportedCert(
	ctx context.Context,
	storage certmagic.Storage,
	issuerKey, hostname string,
	keyPEM, certPEM []byte,
) error {
	// The shape of certmagic.CertificateResource, which has no issuer
	// data for a certificate it did not order.
	meta, err := json.Marshal(struct {
		SANs []string `json:"sans"`
	}{SANs: []string{hostname}})
	if err != nil {
		return fmt.Errorf("encoding certificate metadata: %w", err)
	}

	items := []struct {
		key   string
		value []byte
	}{
		{certmagic.StorageKeys.SitePrivateKey(issuerKey, hostname), keyPEM},
		{certmagic.StorageKeys.SiteMeta(issuerKey, hostname), meta},
		// The certificate last: its presence is what marks the import done.
		{certmagic.StorageKeys.SiteCert(issuerKey, hostname), certPEM},
	}

	for _, item := range items {
		err = storage.Store(ctx, item.key, item.value)
		if err != nil {
			return fmt.Errorf("storing %s: %w", item.key, err)
		}
	}

	return nil
}
