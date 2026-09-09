package apiv1

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/egress"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMapErrorHidesUnmappedErrors proves an error the API does not recognise
// reaches the caller as an id only: the text of a database or filesystem error
// names tables, paths and hosts the caller did not ask about. The mapped
// branches keep saying what is wrong, because the caller can act on those.
func TestMapErrorHidesUnmappedErrors(t *testing.T) {
	t.Parallel()

	err := mapError("getting node", errors.New("sqlite: no such table: nodes at /var/lib/headscale/db.sqlite"))

	statusErr, ok := err.(huma.StatusError) //nolint:errorlint // huma returns the interface, not a wrapped error
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, statusErr.GetStatus())
	assert.NotContains(t, err.Error(), "sqlite")
	assert.NotContains(t, err.Error(), "/var/lib/headscale")
	assert.Regexp(t, `^internal error, see the server log for id [0-9a-f]{8}$`, err.Error())

	// Two errors get two ids, so a log line matches one response.
	other := mapError("getting node", errors.New("another failure"))
	assert.NotEqual(t, err.Error(), other.Error())

	// A mapped error still explains itself.
	mapped := mapError("getting node", types.ErrWebhookNotFound)

	statusErr, ok = mapped.(huma.StatusError) //nolint:errorlint // as above
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, statusErr.GetStatus())

	// The egress guard is invalid input, not a server fault.
	blocked := mapError("creating webhook", egress.ErrBlocked)

	statusErr, ok = blocked.(huma.StatusError) //nolint:errorlint // as above
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}
