package ingress

import (
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListenAllNamesTheTakenAddress proves an address another socket holds
// fails with the setting and address named, the errno reachable, and the
// listeners opened before it closed again.
func TestListenAllNamesTheTakenAddress(t *testing.T) {
	t.Parallel()

	taken, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = taken.Close() })

	free, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	freeAddr := free.Addr().String()
	require.NoError(t, free.Close())

	_, err = listenAll(t.Context(), []string{freeAddr, taken.Addr().String()}, "funnel.listen_addrs")
	require.Error(t, err)
	require.ErrorIs(t, err, syscall.EADDRINUSE)

	bindErr, ok := errors.AsType[*types.ListenerBindError](err)
	require.True(t, ok, "want a ListenerBindError, got %T", err)
	assert.Equal(t, "Funnel ingress", bindErr.Listener)
	assert.Equal(t, "funnel.listen_addrs", bindErr.ConfigKey)
	assert.Equal(t, taken.Addr().String(), bindErr.Addr)

	again, err := new(net.ListenConfig).Listen(t.Context(), "tcp", freeAddr)
	require.NoError(t, err, "the listener opened before the failure must be closed")
	require.NoError(t, again.Close())
}
