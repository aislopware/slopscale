package state

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// authSourcesTestState is a State whose auth cache uses tuning, closed at the
// end of the test.
func authSourcesTestState(t *testing.T, tuning func(*types.Tuning)) *State {
	t.Helper()

	cfg := persistTestConfig(t.TempDir() + "/slopscale.db")
	if tuning != nil {
		tuning(&cfg.Tuning)
	}

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s
}

// pendingRegistration is an unauthenticated registration as handleRegister
// parks it: a fresh machine key, node key and auth ID.
func pendingRegistration(hostname string) (types.AuthID, *types.AuthRequest) {
	return types.MustAuthID(), types.NewRegisterAuthRequest(&types.RegistrationData{
		MachineKey: key.NewMachine().Public(),
		NodeKey:    key.NewNode().Public(),
		Hostname:   hostname,
		Hostinfo:   &tailcfg.Hostinfo{Hostname: hostname},
	})
}

// fillSource parks n registrations from addrs (cycling through them) and
// returns their IDs; every one must be accepted.
func fillSource(t *testing.T, s *State, n int, addrs ...netip.Addr) []types.AuthID {
	t.Helper()

	ids := make([]types.AuthID, 0, n)

	for i := range n {
		id, req := pendingRegistration(fmt.Sprintf("fill-%d", i))
		require.NoError(t, s.SetPendingRegistration(id, req, addrs[i%len(addrs)]))

		ids = append(ids, id)
	}

	return ids
}

func requireRefused(t *testing.T, s *State, source netip.Addr) {
	t.Helper()

	id, req := pendingRegistration("refused")
	err := s.SetPendingRegistration(id, req, source)
	require.ErrorIs(t, err, ErrTooManyPendingRegistrations)

	_, ok := s.GetAuthCacheEntry(id)
	require.False(t, ok, "a refused registration must not be stored")
}

func requireAccepted(t *testing.T, s *State, source netip.Addr) {
	t.Helper()

	id, req := pendingRegistration("accepted")
	require.NoError(t, s.SetPendingRegistration(id, req, source))

	_, ok := s.GetAuthCacheEntry(id)
	require.True(t, ok)
}

// TestPendingRegistrationFloodCannotEvictOtherSource is the "Tailworm"
// report's pre-authentication onboarding denial: one network opening
// registrations with fresh machine keys used to push every other client's
// pending login out of the bounded auth cache. With a cache of 32 and 2000
// attempts, the victim's entry would be long gone without the per-source
// limit.
func TestPendingRegistrationFloodCannotEvictOtherSource(t *testing.T) {
	t.Parallel()

	s := authSourcesTestState(t, func(tu *types.Tuning) {
		tu.RegisterCacheMaxEntries = 32
		tu.RegisterCacheMaxPerSource = 16
	})

	victimID, victimReq := pendingRegistration("victim")
	require.NoError(t, s.SetPendingRegistration(victimID, victimReq, netip.MustParseAddr("192.0.2.10")))

	attacker := netip.MustParseAddr("198.51.100.7")
	accepted, refused := 0, 0

	for i := range 2000 {
		id, req := pendingRegistration(fmt.Sprintf("flood-%d", i))

		err := s.SetPendingRegistration(id, req, attacker)
		if err != nil {
			require.ErrorIs(t, err, ErrTooManyPendingRegistrations)

			refused++

			continue
		}

		accepted++
	}

	assert.Equal(t, 16, accepted, "the attacker's network gets its share and no more")
	assert.Equal(t, 2000-16, refused)

	got, ok := s.GetAuthCacheEntry(victimID)
	require.True(t, ok, "the victim's pending registration must survive the flood")
	require.Same(t, victimReq, got)

	select {
	case verdict := <-victimReq.WaitForAuth():
		t.Fatalf("victim's registration was resolved during the flood: %+v", verdict)
	default:
	}

	assert.Equal(t, map[string]int{"192.0.2.10/32": 1, "198.51.100.7/32": 16}, s.regSources.snapshot())
}

// TestPendingRegistrationRefusedAtSourceCap: the limit refuses only the full
// network; other networks and authenticated entries are not affected.
func TestPendingRegistrationRefusedAtSourceCap(t *testing.T) {
	t.Parallel()

	s := authSourcesTestState(t, func(tu *types.Tuning) { tu.RegisterCacheMaxPerSource = 4 })

	full := netip.MustParseAddr("203.0.113.5")
	fillSource(t, s, 4, full)

	requireRefused(t, s, full)
	requireRefused(t, s, netip.MustParseAddr("::ffff:203.0.113.5")) // the same IPv4 source, IPv4-mapped
	requireAccepted(t, s, netip.MustParseAddr("203.0.113.6"))

	// Authenticated entries (API registrations, SSH check holds) carry no
	// source and are never limited.
	for range 8 {
		id, req := pendingRegistration("api")
		s.SetAuthCacheEntry(id, req)

		_, ok := s.GetAuthCacheEntry(id)
		require.True(t, ok)
	}

	// An entry without a known source is not counted either.
	requireAccepted(t, s, netip.Addr{})

	assert.Equal(t, map[string]int{"203.0.113.5/32": 4, "203.0.113.6/32": 1}, s.regSources.snapshot())
}

// TestPendingRegistrationIPv6SourceIsSlash64: addresses inside one /64 share
// a limit, since whoever holds the /64 picks any of them for free; the next
// /64 has its own.
func TestPendingRegistrationIPv6SourceIsSlash64(t *testing.T) {
	t.Parallel()

	s := authSourcesTestState(t, func(tu *types.Tuning) { tu.RegisterCacheMaxPerSource = 4 })

	fillSource(t, s, 4,
		netip.MustParseAddr("2001:db8:1:2::1"),
		netip.MustParseAddr("2001:db8:1:2:aaaa::2"),
		netip.MustParseAddr("2001:db8:1:2:ffff:ffff:ffff:ffff"),
		netip.MustParseAddr("fe80::1%eth0"), // link-local, its own /64
	)

	requireAccepted(t, s, netip.MustParseAddr("2001:db8:1:2:1234::5678"))
	requireRefused(t, s, netip.MustParseAddr("2001:db8:1:2:dead:beef::1"))
	requireAccepted(t, s, netip.MustParseAddr("2001:db8:1:3::1"))

	assert.Equal(t, map[string]int{
		"2001:db8:1:2::/64": 4,
		"2001:db8:1:3::/64": 1,
		"fe80::/64":         1,
	}, s.regSources.snapshot())
}

// TestPendingRegistrationSlotFreedOnCompletionAndRemove: a completed
// registration ([State.HandleNodeFromAuthPath] removes its entry) and a
// removed one give their slot back.
func TestPendingRegistrationSlotFreedOnCompletionAndRemove(t *testing.T) {
	t.Parallel()

	s := authSourcesTestState(t, func(tu *types.Tuning) { tu.RegisterCacheMaxPerSource = 2 })
	user := s.CreateUserForTest("source-user")

	source := netip.MustParseAddr("192.0.2.44")
	ids := fillSource(t, s, 2, source)
	requireRefused(t, s, source)

	_, _, err := s.HandleNodeFromAuthPath(ids[0], types.UserID(user.ID), nil, util.RegisterMethodCLI)
	require.NoError(t, err)

	_, ok := s.GetAuthCacheEntry(ids[0])
	require.False(t, ok, "a completed registration leaves the cache")

	requireAccepted(t, s, source)
	requireRefused(t, s, source)

	s.DeleteAuthCacheEntryForTest(ids[1])
	requireAccepted(t, s, source)
	requireRefused(t, s, source)
}

// TestPendingRegistrationSlotFreedOnExpiry: an entry that times out gives its
// slot back, and its waiter still learns the registration expired.
func TestPendingRegistrationSlotFreedOnExpiry(t *testing.T) {
	t.Parallel()

	s := authSourcesTestState(t, func(tu *types.Tuning) {
		tu.RegisterCacheMaxPerSource = 1
		tu.RegisterCacheExpiration = 200 * time.Millisecond
	})

	source := netip.MustParseAddr("192.0.2.45")

	id, req := pendingRegistration("expiring")
	require.NoError(t, s.SetPendingRegistration(id, req, source))
	requireRefused(t, s, source)

	select {
	case verdict := <-req.WaitForAuth():
		require.ErrorIs(t, verdict.Err, ErrRegistrationExpired)
	case <-time.After(10 * time.Second):
		t.Fatal("the pending registration never expired")
	}

	// The callback releases the slot before it resolves the verdict, so the
	// slot is free once the waiter has woken.
	assert.Empty(t, s.regSources.snapshot())
	requireAccepted(t, s, source)
}

// TestPendingRegistrationSlotFreedOnLRUEviction keeps the count exact when the
// global cap evicts an entry, and when one ID is stored twice.
func TestPendingRegistrationSlotFreedOnLRUEviction(t *testing.T) {
	t.Parallel()

	s := authSourcesTestState(t, func(tu *types.Tuning) {
		tu.RegisterCacheMaxEntries = 4
		tu.RegisterCacheMaxPerSource = 16
	})

	first := netip.MustParseAddr("192.0.2.1")
	ids := fillSource(t, s, 4, first)

	// Storing an ID again replaces the entry without counting it twice.
	_, again := pendingRegistration("again")
	require.NoError(t, s.SetPendingRegistration(ids[3], again, first))
	assert.Equal(t, map[string]int{"192.0.2.1/32": 4}, s.regSources.snapshot())

	// The cache is full: the next entry evicts the oldest one, whose source
	// gets its slot back.
	requireAccepted(t, s, netip.MustParseAddr("192.0.2.2"))
	assert.Equal(t, map[string]int{"192.0.2.1/32": 3, "192.0.2.2/32": 1}, s.regSources.snapshot())

	_, ok := s.GetAuthCacheEntry(ids[0])
	assert.False(t, ok, "the oldest entry was evicted")
}

func TestSourceNetwork(t *testing.T) {
	t.Parallel()

	for addr, want := range map[string]string{
		"192.0.2.7":               "192.0.2.7/32",
		"::ffff:192.0.2.7":        "192.0.2.7/32",
		"2001:db8:a:b:c:d:e:f":    "2001:db8:a:b::/64",
		"2001:db8:a:b::1":         "2001:db8:a:b::/64",
		"fe80::1234%eth0":         "fe80::/64",
		"2001:db8:a:c:ffff::ffff": "2001:db8:a:c::/64",
	} {
		assert.Equal(t, want, sourceNetwork(netip.MustParseAddr(addr)).String(), addr)
	}
}
