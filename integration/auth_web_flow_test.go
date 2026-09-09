package integration

import (
	"net/netip"
	"slices"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/integration/hsic"
	"github.com/aislopware/slopscale/integration/integrationutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthWebFlowAuthenticationPingAll(t *testing.T) {
	IntegrationSkip(t)

	spec := ScenarioSpec{
		NodesPerUser: len(MustTestVersions),
		Users:        []string{"user1", "user2"},
	}

	scenario, err := NewScenario(spec)
	if err != nil {
		t.Fatalf("failed to create scenario: %s", err)
	}
	defer scenario.ShutdownAssertNoPanics(t)

	err = scenario.CreateSlopscaleEnvWithLoginURL(
		nil,
		hsic.WithTestName("webauthping"),
	)
	requireNoErrSlopscaleEnv(t, err)

	allClients, err := scenario.ListTailscaleClients()
	requireNoErrListClients(t, err)

	allIps, err := scenario.ListTailscaleClientsIPs()
	requireNoErrListClientIPs(t, err)

	err = scenario.WaitForTailscaleSync()
	requireNoErrSync(t, err)

	allAddrs := lo.Map(allIps, func(x netip.Addr, _ int) string {
		return x.String()
	})

	assertPingAll(t, allClients, allAddrs)
}

func TestAuthWebFlowLogoutAndReloginSameUser(t *testing.T) {
	IntegrationSkip(t)

	spec := ScenarioSpec{
		NodesPerUser: len(MustTestVersions),
		Users:        []string{"user1", "user2"},
	}

	scenario, err := NewScenario(spec)

	require.NoError(t, err)
	defer scenario.ShutdownAssertNoPanics(t)

	err = scenario.CreateSlopscaleEnvWithLoginURL(
		nil,
		hsic.WithTestName("weblogout"),
	)
	requireNoErrSlopscaleEnv(t, err)

	allClients, err := scenario.ListTailscaleClients()
	requireNoErrListClients(t, err)

	allIps, err := scenario.ListTailscaleClientsIPs()
	requireNoErrListClientIPs(t, err)

	err = scenario.WaitForTailscaleSync()
	requireNoErrSync(t, err)

	allAddrs := lo.Map(allIps, func(x netip.Addr, _ int) string {
		return x.String()
	})

	assertPingAll(t, allClients, allAddrs)

	slopscale, err := scenario.Slopscale()
	requireNoErrGetSlopscale(t, err)

	// Collect expected node IDs for validation
	expectedNodes := collectExpectedNodeIDs(t, allClients)

	// Validate initial connection state
	validateInitialConnection(t, slopscale, expectedNodes)

	var listNodes []*clientv1.Node

	t.Logf("Validating initial node count after web auth at %s", time.Now().Format(TimestampFormat))
	assert.EventuallyWithT(t, func(ct *assert.CollectT) {
		var listNodesErr error

		listNodes, listNodesErr = slopscale.ListNodes()
		assert.NoError(ct, listNodesErr, "Failed to list nodes after web authentication")
		assert.Len(
			ct,
			listNodes,
			len(allClients),
			"Expected %d nodes after web auth, got %d",
			len(allClients),
			len(listNodes),
		)
	},
		integrationutil.StatusReadyTimeout,
		2*time.Second,
		"validating node count matches client count after web authentication",
	)

	nodeCountBeforeLogout := len(listNodes)
	t.Logf("node count before logout: %d", nodeCountBeforeLogout)

	clientIPs := make(map[TailscaleClient][]netip.Addr)

	for _, client := range allClients {
		ips, iPsErr := client.IPs()
		if iPsErr != nil {
			t.Fatalf("failed to get IPs for client %s: %s", client.Hostname(), iPsErr)
		}

		clientIPs[client] = ips
	}

	for _, client := range allClients {
		logoutErr := client.Logout()
		if logoutErr != nil {
			t.Fatalf("failed to logout client %s: %s", client.Hostname(), logoutErr)
		}
	}

	err = scenario.WaitForTailscaleLogout()
	requireNoErrLogout(t, err)

	// Validate that all nodes are offline after logout
	validateLogoutComplete(t, slopscale, expectedNodes)

	t.Logf("all clients logged out")

	for _, userName := range spec.Users {
		err = scenario.RunTailscaleUpWithURL(userName, slopscale.GetEndpoint())
		if err != nil {
			t.Fatalf("failed to run tailscale up (%q): %s", slopscale.GetEndpoint(), err)
		}
	}

	t.Logf("all clients logged in again")

	t.Logf("Validating node persistence after logout at %s", time.Now().Format(TimestampFormat))
	assert.EventuallyWithT(t, func(ct *assert.CollectT) {
		var listNodesErr error

		listNodes, listNodesErr = slopscale.ListNodes()
		assert.NoError(ct, listNodesErr, "Failed to list nodes after web flow logout")
		assert.Len(
			ct,
			listNodes,
			nodeCountBeforeLogout,
			"Node count should remain unchanged after logout - expected %d nodes, got %d",
			nodeCountBeforeLogout,
			len(listNodes),
		)
	},
		integrationutil.HAConvergeTimeout,
		2*time.Second,
		"validating node persistence in database after web flow logout",
	)
	t.Logf("node count first login: %d, after relogin: %d", nodeCountBeforeLogout, len(listNodes))

	// Validate connection state after relogin
	validateReloginComplete(t, slopscale, expectedNodes)

	allIps, err = scenario.ListTailscaleClientsIPs()
	requireNoErrListClientIPs(t, err)

	allAddrs = lo.Map(allIps, func(x netip.Addr, _ int) string {
		return x.String()
	})

	assertPingAll(t, allClients, allAddrs)

	for _, client := range allClients {
		ips, err := client.IPs()
		if err != nil {
			t.Fatalf("failed to get IPs for client %s: %s", client.Hostname(), err)
		}

		// lets check if the IPs are the same
		if len(ips) != len(clientIPs[client]) {
			t.Fatalf("IPs changed for client %s", client.Hostname())
		}

		for _, ip := range ips {
			found := slices.Contains(clientIPs[client], ip)

			if !found {
				t.Fatalf(
					"IPs changed for client %s. Used to be %v now %v",
					client.Hostname(),
					clientIPs[client],
					ips,
				)
			}
		}
	}

	t.Logf("all clients IPs are the same")
}

// TestAuthWebFlowLogoutAndReloginNewUser tests the scenario where multiple Tailscale clients
// initially authenticate using the web-based authentication flow (where users visit a URL
// in their browser to authenticate), then all clients log out and log back in as a different user.
//
// This test validates the "user switching" behavior in slopscale's web authentication flow:
// - Multiple clients authenticate via web flow, each to their respective users (user1, user2)
// - All clients log out simultaneously
// - All clients log back in via web flow, but this time they all authenticate as user1
// - The test verifies that user1 ends up with all the client nodes
// - The test verifies that user2's original nodes still exist in the database but are offline
// - The test verifies network connectivity works after the user switch
//
// This scenario is important for organizations that need to reassign devices between users
// or when consolidating multiple user accounts. It ensures that slopscale properly handles
// the security implications of user switching while maintaining node persistence in the database.
//
// The test uses slopscale's web authentication flow, which is the most user-friendly method
// where authentication happens through a web browser rather than pre-shared keys or OIDC.
func TestAuthWebFlowLogoutAndReloginNewUser(t *testing.T) {
	IntegrationSkip(t)

	spec := ScenarioSpec{
		NodesPerUser: len(MustTestVersions),
		Users:        []string{"user1", "user2"},
	}

	scenario, err := NewScenario(spec)

	require.NoError(t, err)
	defer scenario.ShutdownAssertNoPanics(t)

	err = scenario.CreateSlopscaleEnvWithLoginURL(
		nil,
		hsic.WithTestName("webflowrelnewuser"),
	)
	requireNoErrSlopscaleEnv(t, err)

	allClients, err := scenario.ListTailscaleClients()
	requireNoErrListClients(t, err)

	var allIps []netip.Addr

	allIps, err = scenario.ListTailscaleClientsIPs()
	requireNoErrListClientIPs(t, err)

	_ = allIps // used below after user switch

	err = scenario.WaitForTailscaleSync()
	requireNoErrSync(t, err)

	slopscale, err := scenario.Slopscale()
	requireNoErrGetSlopscale(t, err)

	// Collect expected node IDs for validation
	expectedNodes := collectExpectedNodeIDs(t, allClients)

	// Validate initial connection state
	validateInitialConnection(t, slopscale, expectedNodes)

	var listNodes []*clientv1.Node

	t.Logf("Validating initial node count after web auth at %s", time.Now().Format(TimestampFormat))
	assert.EventuallyWithT(t, func(ct *assert.CollectT) {
		var listNodesErr error

		listNodes, listNodesErr = slopscale.ListNodes()
		assert.NoError(ct, listNodesErr, "Failed to list nodes after initial web authentication")
		assert.Len(
			ct,
			listNodes,
			len(allClients),
			"Expected %d nodes after web auth, got %d",
			len(allClients),
			len(listNodes),
		)
	},
		integrationutil.StatusReadyTimeout,
		2*time.Second,
		"validating node count matches client count after initial web authentication",
	)

	nodeCountBeforeLogout := len(listNodes)
	t.Logf("node count before logout: %d", nodeCountBeforeLogout)

	// Log out all clients
	for _, client := range allClients {
		logoutErr := client.Logout()
		if logoutErr != nil {
			t.Fatalf("failed to logout client %s: %s", client.Hostname(), logoutErr)
		}
	}

	err = scenario.WaitForTailscaleLogout()
	requireNoErrLogout(t, err)

	// Validate that all nodes are offline after logout
	validateLogoutComplete(t, slopscale, expectedNodes)

	t.Logf("all clients logged out")

	// Log all clients back in as user1 using web flow
	// We manually iterate over all clients and authenticate each one as user1
	// This tests the cross-user re-authentication behavior where ALL clients
	// (including those originally from user2) are registered to user1
	for _, client := range allClients {
		loginURL, getEndpointErr := client.LoginWithURL(slopscale.GetEndpoint())
		if getEndpointErr != nil {
			t.Fatalf("failed to get login URL for client %s: %s", client.Hostname(), getEndpointErr)
		}

		body, getEndpointErr := doLoginURL(client.Hostname(), loginURL)
		if getEndpointErr != nil {
			t.Fatalf("failed to complete login for client %s: %s", client.Hostname(), getEndpointErr)
		}

		// Register all clients as user1 (this is where cross-user registration happens)
		// This simulates: slopscale auth register --auth-id <id> --user user1
		_ = scenario.runSlopscaleRegister("user1", body)
	}

	// Wait for all clients to reach running state
	for _, client := range allClients {
		peerSyncTimeoutErr := client.WaitForRunning(integrationutil.PeerSyncTimeout())
		if peerSyncTimeoutErr != nil {
			t.Fatalf("%s tailscale node has not reached running: %s", client.Hostname(), peerSyncTimeoutErr)
		}
	}

	t.Logf("all clients logged back in as user1")

	var user1Nodes []*clientv1.Node

	t.Logf("Validating user1 node count after relogin at %s", time.Now().Format(TimestampFormat))
	assert.EventuallyWithT(t, func(ct *assert.CollectT) {
		var listNodesErr error

		user1Nodes, listNodesErr = slopscale.ListNodes("user1")
		assert.NoError(ct, listNodesErr, "Failed to list nodes for user1 after web flow relogin")
		assert.Len(
			ct,
			user1Nodes,
			len(allClients),
			"User1 should have all %d clients after web flow relogin, got %d nodes",
			len(allClients),
			len(user1Nodes),
		)
	},
		integrationutil.HAConvergeTimeout,
		2*time.Second,
		"validating user1 has all client nodes after web flow user switch relogin",
	)

	// Collect expected node IDs for user1 after relogin
	expectedUser1Nodes := make([]types.NodeID, 0, len(user1Nodes))
	for _, node := range user1Nodes {
		expectedUser1Nodes = append(expectedUser1Nodes, types.NodeID(mustParseID(node.Id)))
	}

	// Validate connection state after relogin as user1
	validateReloginComplete(t, slopscale, expectedUser1Nodes)

	// Validate that user2's old nodes still exist in database (but are expired/offline)
	// When CLI registration creates new nodes for user1, user2's old nodes remain
	var user2Nodes []*clientv1.Node

	t.Logf(
		"Validating user2 old nodes remain in database after CLI registration to user1 at %s",
		time.Now().Format(TimestampFormat),
	)
	assert.EventuallyWithT(t, func(ct *assert.CollectT) {
		var listNodesErr error

		user2Nodes, listNodesErr = slopscale.ListNodes("user2")
		assert.NoError(ct, listNodesErr, "Failed to list nodes for user2 after CLI registration to user1")
		assert.Len(
			ct,
			user2Nodes,
			len(allClients)/2,
			"User2 should still have %d old nodes (likely expired) after CLI registration to user1, got %d nodes",
			len(allClients)/2,
			len(user2Nodes),
		)
	},
		integrationutil.StatusReadyTimeout,
		2*time.Second,
		"validating user2 old nodes remain in database after CLI registration to user1",
	)

	t.Logf("Validating client login states after web flow user switch at %s", time.Now().Format(TimestampFormat))

	for _, client := range allClients {
		assert.EventuallyWithT(t, func(ct *assert.CollectT) {
			status, statusErr := client.Status()
			assert.NoError(ct, statusErr, "Failed to get status for client %s", client.Hostname())
			assert.Equal(
				ct,
				"user1@test.no",
				status.User[status.Self.UserID].LoginName,
				"Client %s should be logged in as user1 after web flow user switch, got %s",
				client.Hostname(),
				status.User[status.Self.UserID].LoginName,
			)
		},
			integrationutil.StatusReadyTimeout,
			2*time.Second,
			"validating %s is logged in as user1 after web flow user switch",
			client.Hostname(),
		)
	}

	// Test connectivity after user switch
	allIps, err = scenario.ListTailscaleClientsIPs()
	requireNoErrListClientIPs(t, err)

	allAddrs := lo.Map(allIps, func(x netip.Addr, _ int) string {
		return x.String()
	})

	assertPingAll(t, allClients, allAddrs)
}
