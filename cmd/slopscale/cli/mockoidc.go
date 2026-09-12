package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/mockoidc"
	"github.com/aislopware/slopscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	errMockOidcClientIDNotDefined     = errors.New("MOCKOIDC_CLIENT_ID not defined")
	errMockOidcClientSecretNotDefined = errors.New("MOCKOIDC_CLIENT_SECRET not defined")
	errMockOidcPortNotDefined         = errors.New("MOCKOIDC_PORT not defined")
	errMockOidcUsersNotDefined        = errors.New("MOCKOIDC_USERS not defined")
)

const refreshTTL = 60 * time.Minute

var accessTTL = 2 * time.Minute

func init() {
	rootCmd.AddCommand(mockOidcCmd)
}

var mockOidcCmd = &cobra.Command{
	Use:   "mockoidc",
	Short: "Run a mock OIDC server for testing",
	Long:  "Run an OpenID Connect provider that accepts any login, for tests.",
	RunE: func(_ *cobra.Command, _ []string) error {
		err := mockOIDC()
		if err != nil {
			return fmt.Errorf("running mock OIDC server: %w", err)
		}

		return nil
	},
}

func mockOIDC() error {
	clientID := os.Getenv("MOCKOIDC_CLIENT_ID")
	if clientID == "" {
		return errMockOidcClientIDNotDefined
	}

	clientSecret := os.Getenv("MOCKOIDC_CLIENT_SECRET")
	if clientSecret == "" {
		return errMockOidcClientSecretNotDefined
	}

	addrStr := os.Getenv("MOCKOIDC_ADDR")
	if addrStr == "" {
		return errMockOidcPortNotDefined
	}

	portStr := os.Getenv("MOCKOIDC_PORT")
	if portStr == "" {
		return errMockOidcPortNotDefined
	}

	accessTTLOverride := os.Getenv("MOCKOIDC_ACCESS_TTL")
	if accessTTLOverride != "" {
		newTTL, err := time.ParseDuration(accessTTLOverride)
		if err != nil {
			return fmt.Errorf("parsing mock OIDC access TTL %q: %w", accessTTLOverride, err)
		}

		accessTTL = newTTL
	}

	userStr := os.Getenv("MOCKOIDC_USERS")
	if userStr == "" {
		return errMockOidcUsersNotDefined
	}

	var users []mockoidc.User

	err := json.Unmarshal([]byte(userStr), &users)
	if err != nil {
		return fmt.Errorf("unmarshalling users: %w", err)
	}

	log.Info().Interface(zf.Users, users).Msg("loading users from JSON")

	log.Info().Msgf("access token TTL: %s", accessTTL)

	_, err = strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("parsing mock OIDC port %q: %w", portStr, err)
	}

	mock, err := newMockOIDC(clientID, clientSecret, users)
	if err != nil {
		return err
	}

	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", net.JoinHostPort(addrStr, portStr))
	if err != nil {
		return fmt.Errorf("listening on mock OIDC address: %w", err)
	}

	err = mock.Start(listener, nil)
	if err != nil {
		return fmt.Errorf("starting mock OIDC server: %w", err)
	}

	log.Info().Msgf("mock OIDC server listening on %s", listener.Addr().String())
	log.Info().Msgf("issuer: %s", mock.Issuer())

	c := make(chan struct{})
	<-c

	return nil
}

func newMockOIDC(clientID, clientSecret string, users []mockoidc.User) (*mockoidc.Server, error) {
	mock, err := mockoidc.NewServer()
	if err != nil {
		return nil, fmt.Errorf("creating mock OIDC server: %w", err)
	}

	mock.ClientID = clientID
	mock.ClientSecret = clientSecret
	mock.AccessTTL = accessTTL
	mock.RefreshTTL = refreshTTL

	for _, user := range users {
		mock.QueueUser(user)
	}

	mock.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Info().Str("method", r.Method).Str("url", r.URL.String()).Msg("request")
			h.ServeHTTP(w, r)
		})
	})

	return mock, nil
}
