package db

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPostgresDSNThroughPgx feeds PostgresConfig.DSN to the parser that
// openPostgres uses, so each value must come back exactly as configured.
func TestPostgresDSNThroughPgx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     types.PostgresConfig
		host    string
		port    uint16
		tls     bool
		runtime map[string]string
	}{
		{
			name: "ssl false disables tls",
			cfg: types.PostgresConfig{
				Host: "localhost", Port: 5432, Name: "slopscale", User: "hs", Pass: "secret", Ssl: "false",
			},
			host: "localhost", port: 5432,
		},
		{
			name: "ssl true keeps the driver default",
			cfg:  types.PostgresConfig{Host: "db", Name: "slopscale", User: "hs", Ssl: "true"},
			host: "db", port: 5432, tls: true,
		},
		{
			name: "raw sslmode is passed through",
			cfg:  types.PostgresConfig{Host: "db", Name: "slopscale", User: "hs", Ssl: "require"},
			host: "db", port: 5432, tls: true,
		},
		// The DSN used to be concatenated, so a space or quote in the
		// password cut it short or added parameters.
		{
			name: "reserved characters survive",
			cfg: types.PostgresConfig{
				Host: "db",
				Name: "slop scale",
				User: "h's",
				Pass: `p a'ss\w sslmode=disable application_name=x #?&@`,
				Ssl:  "false",
			},
			host: "db", port: 5432,
		},
		{
			name: "unix socket directory",
			cfg: types.PostgresConfig{
				Host: "/var/run/postgresql", Port: 5433, Name: "slopscale", User: "hs", Ssl: "false",
			},
			host: "/var/run/postgresql", port: 5433,
		},
		{
			name: "bare ipv6 host",
			cfg:  types.PostgresConfig{Host: "::1", Name: "slopscale", User: "hs", Ssl: "false"},
			host: "::1", port: 5432,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := pgxpool.ParseConfig(tt.cfg.DSN())
			require.NoError(t, err)

			conn := parsed.ConnConfig
			assert.Equal(t, tt.host, conn.Host)
			assert.Equal(t, tt.port, conn.Port)
			assert.Equal(t, tt.cfg.Name, conn.Database)
			assert.Equal(t, tt.cfg.User, conn.User)
			assert.Equal(t, tt.cfg.Pass, conn.Password)
			assert.Equal(t, tt.tls, conn.TLSConfig != nil, "tls")
			assert.Empty(t, conn.RuntimeParams["application_name"], "password leaked a parameter")
		})
	}
}
