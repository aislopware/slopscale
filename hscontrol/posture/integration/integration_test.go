package integration

import (
	"net/http"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("unknown provider", func(t *testing.T) {
		t.Parallel()

		_, err := New(types.PostureIntegration{Provider: "unknown"}, nil)
		require.ErrorIs(t, err, ErrProvider)
	})

	t.Run("default http client", func(t *testing.T) {
		t.Parallel()

		c, err := New(types.PostureIntegration{Provider: types.PostureProviderFalcon}, nil)
		require.NoError(t, err)
		assert.NotNil(t, c)
	})

	for _, p := range types.PostureProviders {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			c, err := New(types.PostureIntegration{Provider: p}, http.DefaultClient)
			require.NoError(t, err)
			assert.NotNil(t, c)
		})
	}
}
