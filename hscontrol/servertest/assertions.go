package servertest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// convergeTimeout bounds how long the mesh assertions wait for every
// client's netmap to settle. Peer lists arrive over several map responses,
// so a snapshot taken right after a change would report a state that is
// still in flight.
const convergeTimeout = 10 * time.Second

// AssertMeshComplete verifies that every client in the slice sees
// exactly (len(clients) - 1) peers, i.e. a fully connected mesh.
func AssertMeshComplete(tb testing.TB, clients []*TestClient) {
	tb.Helper()

	expected := len(clients) - 1

	assert.EventuallyWithT(tb, func(collect *assert.CollectT) {
		for _, c := range clients {
			nm := c.Netmap()
			if nm == nil {
				collect.Errorf("AssertMeshComplete: %s has no netmap", c.Name)

				continue
			}

			if got := len(nm.Peers); got != expected {
				collect.Errorf("AssertMeshComplete: %s has %d peers, want %d (peers: %v)",
					c.Name, got, expected, c.PeerNames())
			}
		}
	}, convergeTimeout, 25*time.Millisecond)
}

// AssertSymmetricVisibility checks that peer visibility is symmetric:
// if client A sees client B, then client B must also see client A.
func AssertSymmetricVisibility(tb testing.TB, clients []*TestClient) {
	tb.Helper()

	assert.EventuallyWithT(tb, func(collect *assert.CollectT) {
		for _, a := range clients {
			for _, b := range clients {
				if a == b {
					continue
				}

				_, aSeesB := a.PeerByName(b.Name)

				_, bSeesA := b.PeerByName(a.Name)
				if aSeesB != bSeesA {
					collect.Errorf("AssertSymmetricVisibility: %s sees %s = %v, but %s sees %s = %v",
						a.Name, b.Name, aSeesB, b.Name, a.Name, bSeesA)
				}
			}
		}
	}, convergeTimeout, 25*time.Millisecond)
}

// AssertPeerOnline checks that the observer sees peerName as online.
func AssertPeerOnline(tb testing.TB, observer *TestClient, peerName string) {
	tb.Helper()

	assert.EventuallyWithT(tb, func(collect *assert.CollectT) {
		peer, ok := observer.PeerByName(peerName)
		if !ok {
			collect.Errorf("AssertPeerOnline: %s does not see peer %s", observer.Name, peerName)

			return
		}

		isOnline, known := peer.Online().GetOk()
		if !known || !isOnline {
			collect.Errorf("AssertPeerOnline: %s sees peer %s but Online=%v (known=%v), want true",
				observer.Name, peerName, isOnline, known)
		}
	}, convergeTimeout, 25*time.Millisecond)
}

// AssertConsistentState checks that all clients agree on peer
// properties: every connected client should see the same set of
// peer hostnames.
func AssertConsistentState(tb testing.TB, clients []*TestClient) {
	tb.Helper()

	assert.EventuallyWithT(tb, func(collect *assert.CollectT) {
		for _, c := range clients {
			nm := c.Netmap()
			if nm == nil {
				continue
			}

			peerNames := make(map[string]bool, len(nm.Peers))
			for _, p := range nm.Peers {
				hi := p.Hostinfo()
				if hi.Valid() {
					peerNames[hi.Hostname()] = true
				}
			}

			// Check that c sees all other connected clients.
			for _, other := range clients {
				if other == c || other.Netmap() == nil {
					continue
				}

				if !peerNames[other.Name] {
					collect.Errorf("AssertConsistentState: %s does not see %s (peers: %v)",
						c.Name, other.Name, c.PeerNames())
				}
			}
		}
	}, convergeTimeout, 25*time.Millisecond)
}

// AssertDERPMapPresent checks that the netmap contains a DERP map.
func AssertDERPMapPresent(tb testing.TB, client *TestClient) {
	tb.Helper()

	nm := client.Netmap()
	if nm == nil {
		tb.Errorf("AssertDERPMapPresent: %s has no netmap", client.Name)

		return
	}

	if nm.DERPMap == nil {
		tb.Errorf("AssertDERPMapPresent: %s has nil DERPMap", client.Name)

		return
	}

	if len(nm.DERPMap.Regions) == 0 {
		tb.Errorf("AssertDERPMapPresent: %s has empty DERPMap regions", client.Name)
	}
}

// AssertSelfHasAddresses checks that the self node has at least one address.
func AssertSelfHasAddresses(tb testing.TB, client *TestClient) {
	tb.Helper()

	nm := client.Netmap()
	if nm == nil {
		tb.Errorf("AssertSelfHasAddresses: %s has no netmap", client.Name)

		return
	}

	if !nm.SelfNode.Valid() {
		tb.Errorf("AssertSelfHasAddresses: %s self node is invalid", client.Name)

		return
	}

	if nm.SelfNode.Addresses().Len() == 0 {
		tb.Errorf("AssertSelfHasAddresses: %s self node has no addresses", client.Name)
	}
}
