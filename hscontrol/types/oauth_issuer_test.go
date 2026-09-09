package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFederatedIssuer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		issuer  string
		host    string
		wantErr error
	}{
		{
			name:   "https",
			raw:    " https://token.actions.githubusercontent.com/ ",
			issuer: "https://token.actions.githubusercontent.com",
			host:   "token.actions.githubusercontent.com",
		},
		{name: "loopback http", raw: "http://127.0.0.1:8443", issuer: "http://127.0.0.1:8443", host: "127.0.0.1:8443"},
		{
			name:   "localhost http",
			raw:    "http://localhost:9100/issuer",
			issuer: "http://localhost:9100/issuer",
			host:   "localhost:9100",
		},
		{name: "public http", raw: "http://idp.example.com", wantErr: ErrIssuerInsecure},
		{name: "no scheme", raw: "idp.example.com", wantErr: ErrIssuerURL},
		{name: "other scheme", raw: "ftp://idp.example.com", wantErr: ErrIssuerURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			issuer, host, err := ParseFederatedIssuer(tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.issuer, issuer)
			assert.Equal(t, tt.host, host)
		})
	}
}
