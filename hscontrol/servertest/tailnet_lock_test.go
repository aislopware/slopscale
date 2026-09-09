package servertest_test

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tka"
	"tailscale.com/types/key"
	"tailscale.com/types/netmap"
	"tailscale.com/types/tkatype"
)

// lockGenesis builds the genesis AUM `tailscale lock init` would, with
// one trusted key and the given disablement secrets.
func lockGenesis(t *testing.T, signer key.NLPrivate, secrets ...[]byte) tka.AUM {
	t.Helper()

	var entropy [16]byte

	_, err := rand.Read(entropy[:])
	require.NoError(t, err)

	values := make([][]byte, 0, len(secrets))
	for _, secret := range secrets {
		values = append(values, tka.DisablementKDF(secret))
	}

	_, genesis, err := tka.Create(tka.ChonkMem(), tka.State{
		Keys:              []tka.Key{{Kind: tka.Key25519, Votes: 1, Public: signer.Public().Verifier()}},
		DisablementValues: values,
		StateID1:          binary.LittleEndian.Uint64(entropy[:8]),
		StateID2:          binary.LittleEndian.Uint64(entropy[8:]),
	}, signer)
	require.NoError(t, err)

	return genesis
}

// signNodeKey signs a node key the way the client does for init and
// `tailscale lock sign`.
func signNodeKey(t *testing.T, info tailcfg.TKASignInfo, signer key.NLPrivate) tkatype.MarshaledSignature {
	t.Helper()

	pub, err := info.NodePublic.MarshalBinary()
	require.NoError(t, err)

	sig := tka.NodeKeySignature{
		SigKind:        tka.SigDirect,
		KeyID:          signer.KeyID(),
		Pubkey:         pub,
		WrappingPubkey: info.RotationPubkey,
	}

	sig.Signature, err = signer.SignNKS(sig.SigHash())
	require.NoError(t, err)

	return sig.Serialize()
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()

	var out T

	require.NoError(t, json.Unmarshal(body, &out), string(body))

	return out
}

// boolField reads a boolean field of an API response.
func boolField(t *testing.T, body any, path ...string) bool {
	t.Helper()

	value, ok := field(t, body, path...).(bool)
	require.True(t, ok, "field %v is not a boolean", path)

	return value
}

func peerSignature(nm *netmap.NetworkMap, name string) []byte {
	for _, p := range nm.Peers {
		if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == name {
			return p.KeySignature().AsSlice()
		}
	}

	return nil
}

// TestTailnetLock proves the lock round trip with a real control client:
// the lock is off and every node holds the cap; an admin's node proposes
// a genesis, gets every node to sign and switches the lock on, after
// which every node learns the head and its signature and peers carry
// theirs; a node bootstraps from the genesis and syncs an added key; a
// node that joins later is unsigned until a signing node signs it; a
// node whose key expires re-signs its new key on its own; and the
// operator switches the lock off with the support secret, which every
// node then fetches. The subtests build on one another.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestTailnetLock(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "lock.test"}))
	owner := srv.CreateUser(t, "alice")
	alice := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	server := servertest.NewClient(t, srv, "srv", servertest.WithTags("tag:server"))
	alice.WaitForPeerCount(t, 1, 10*time.Second)

	client := srv.HTTPClient(t)
	apiKey := srv.CreateAPIKey(t, nil)
	api := srv.URL + "/api/v1/tailnet-lock"

	tkaDo := func(node *servertest.TestClient, path string, body any) (int, []byte) {
		return machineDo(t, srv, node, http.MethodGet, "tka/"+path, body)
	}

	signer := key.NewNLPrivate()
	nodeSecret := bytes.Repeat([]byte{1}, 32)
	supportSecret := bytes.Repeat([]byte{2}, 32)
	genesis := lockGenesis(t, signer, nodeSecret, supportSecret)

	// The side of the authority a signing node keeps.
	chonk := tka.ChonkMem()

	authority, err := tka.Bootstrap(chonk, genesis)
	require.NoError(t, err)

	t.Run("the lock is off and every node may take part", func(t *testing.T) {
		nm := alice.Netmap()
		require.NotNil(t, nm)
		assert.False(t, nm.TKAEnabled)
		assert.Contains(t, nm.SelfNode.CapMap().AsMap(), nodecap.TailnetLock)

		status, body := apiCall(t, client, apiKey, http.MethodGet, api, nil)
		require.Equal(t, http.StatusOK, status)
		assert.False(t, boolField(t, body, "enabled"))
		assert.False(t, boolField(t, body, "supportDisablementAvailable"))
	})

	var need []tailcfg.TKASignInfo

	t.Run("init begin lists every node to sign", func(t *testing.T) {
		status, body := tkaDo(server, "init/begin", tailcfg.TKAInitBeginRequest{
			Version:    tailcfg.CurrentCapabilityVersion,
			NodeKey:    server.NodePrivateKey().Public(),
			GenesisAUM: genesis.Serialize(),
		})
		assert.Equal(t, http.StatusForbidden, status, "a tagged node may not enable the lock: %s", body)

		status, body = tkaDo(alice, "init/begin", tailcfg.TKAInitBeginRequest{
			Version:    tailcfg.CurrentCapabilityVersion,
			NodeKey:    alice.NodePrivateKey().Public(),
			GenesisAUM: genesis.Serialize(),
		})
		require.Equal(t, http.StatusOK, status, string(body))

		resp := decodeJSON[tailcfg.TKAInitBeginResponse](t, body)
		need = resp.NeedSignatures
		require.Len(t, need, 2)

		for _, info := range need {
			assert.NotEmpty(t, info.RotationPubkey, "node %d has a lock key to rotate with", info.NodeID)
		}
	})

	t.Run("init finish switches the lock on", func(t *testing.T) {
		signatures := map[tailcfg.NodeID]tkatype.MarshaledSignature{}
		for _, info := range need {
			signatures[info.NodeID] = signNodeKey(t, info, signer)
		}

		partial := map[tailcfg.NodeID]tkatype.MarshaledSignature{need[0].NodeID: signatures[need[0].NodeID]}

		status, body := tkaDo(alice, "init/finish", tailcfg.TKAInitFinishRequest{
			Version:    tailcfg.CurrentCapabilityVersion,
			NodeKey:    alice.NodePrivateKey().Public(),
			Signatures: partial,
		})
		assert.Equal(t, http.StatusBadRequest, status, "every node must be signed: %s", body)

		status, body = tkaDo(alice, "init/finish", tailcfg.TKAInitFinishRequest{
			Version:            tailcfg.CurrentCapabilityVersion,
			NodeKey:            alice.NodePrivateKey().Public(),
			Signatures:         signatures,
			SupportDisablement: supportSecret,
		})
		require.Equal(t, http.StatusOK, status, string(body))

		head := genesis.Hash().String()

		for _, node := range []*servertest.TestClient{alice, server} {
			node.WaitForCondition(t, "lock on with signatures", 10*time.Second, func(nm *netmap.NetworkMap) bool {
				return nm.TKAEnabled && nm.TKAHead.String() == head &&
					nm.SelfNode.KeySignature().Len() > 0 && len(nm.Peers) == 1 &&
					nm.Peers[0].KeySignature().Len() > 0
			})
		}

		nm := alice.Netmap()
		assert.NoError(t, authority.NodeKeyAuthorized(nm.SelfNode.Key(), nm.SelfNode.KeySignature().AsSlice()))
		assert.NoError(t, authority.NodeKeyAuthorized(nm.Peers[0].Key(), nm.Peers[0].KeySignature().AsSlice()))

		status, body = tkaDo(alice, "init/begin", tailcfg.TKAInitBeginRequest{
			Version:    tailcfg.CurrentCapabilityVersion,
			NodeKey:    alice.NodePrivateKey().Public(),
			GenesisAUM: genesis.Serialize(),
		})
		assert.Equal(t, http.StatusConflict, status, "the lock is already on: %s", body)

		status, apiBody := apiCall(t, client, apiKey, http.MethodGet, api, nil)
		require.Equal(t, http.StatusOK, status)
		assert.True(t, boolField(t, apiBody, "enabled"))
		assert.Equal(t, head, field(t, apiBody, "head"))
		assert.Len(t, field(t, apiBody, "keys"), 1)
		assert.Len(t, field(t, apiBody, "signedNodeIds"), 2)
		assert.Empty(t, field(t, apiBody, "unsignedNodeIds"))
		assert.True(t, boolField(t, apiBody, "supportDisablementAvailable"))
	})

	t.Run("bootstrap hands out the genesis", func(t *testing.T) {
		status, body := tkaDo(server, "bootstrap", tailcfg.TKABootstrapRequest{
			Version: tailcfg.CurrentCapabilityVersion,
			NodeKey: server.NodePrivateKey().Public(),
		})
		require.Equal(t, http.StatusOK, status, string(body))

		resp := decodeJSON[tailcfg.TKABootstrapResponse](t, body)
		assert.Equal(t, genesis.Serialize(), resp.GenesisAUM)
		assert.Empty(t, resp.DisablementSecret)
	})

	second := key.NewNLPrivate()

	t.Run("sync offer and send move the head", func(t *testing.T) {
		offer, err := authority.SyncOffer(chonk)
		require.NoError(t, err)

		head, ancestors, err := tka.FromSyncOffer(offer)
		require.NoError(t, err)

		status, body := tkaDo(alice, "sync/offer", tailcfg.TKASyncOfferRequest{
			Version:   tailcfg.CurrentCapabilityVersion,
			NodeKey:   alice.NodePrivateKey().Public(),
			Head:      head,
			Ancestors: ancestors,
		})
		require.Equal(t, http.StatusOK, status, string(body))

		offerResp := decodeJSON[tailcfg.TKASyncOfferResponse](t, body)
		assert.Equal(t, head, offerResp.Head)
		assert.Empty(t, offerResp.MissingAUMs, "both sides are at the genesis")

		updater := authority.NewUpdater(signer)
		require.NoError(t, updater.AddKey(tka.Key{Kind: tka.Key25519, Votes: 1, Public: second.Public().Verifier()}))

		aums, err := updater.Finalize(chonk)
		require.NoError(t, err)
		require.NoError(t, authority.Inform(chonk, aums))

		missing := make([]tkatype.MarshaledAUM, 0, len(aums))
		for i := range aums {
			missing = append(missing, aums[i].Serialize())
		}

		status, body = tkaDo(alice, "sync/send", tailcfg.TKASyncSendRequest{
			Version:     tailcfg.CurrentCapabilityVersion,
			NodeKey:     alice.NodePrivateKey().Public(),
			Head:        authority.Head().String(),
			MissingAUMs: missing,
		})
		require.Equal(t, http.StatusOK, status, string(body))

		sendResp := decodeJSON[tailcfg.TKASyncSendResponse](t, body)
		assert.Equal(t, authority.Head().String(), sendResp.Head)

		newHead := authority.Head().String()

		server.WaitForCondition(t, "new head", 10*time.Second, func(nm *netmap.NetworkMap) bool {
			return nm.TKAHead.String() == newHead
		})

		// A node still at the genesis is handed the new AUM.
		status, body = tkaDo(server, "sync/offer", tailcfg.TKASyncOfferRequest{
			Version:   tailcfg.CurrentCapabilityVersion,
			NodeKey:   server.NodePrivateKey().Public(),
			Head:      head,
			Ancestors: ancestors,
		})
		require.Equal(t, http.StatusOK, status, string(body))

		offerResp = decodeJSON[tailcfg.TKASyncOfferResponse](t, body)
		assert.Equal(t, newHead, offerResp.Head)
		assert.Len(t, offerResp.MissingAUMs, 1)

		status, apiBody := apiCall(t, client, apiKey, http.MethodGet, api, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Len(t, field(t, apiBody, "keys"), 2)
	})

	// Created on the parent test so the later subtests can use it.
	bob := servertest.NewClient(t, srv, "phone", servertest.WithUser(owner))

	t.Run("a node that joins later is unsigned until signed", func(t *testing.T) {
		bob.WaitForCondition(t, "lock on, unsigned", 10*time.Second, func(nm *netmap.NetworkMap) bool {
			return nm.TKAEnabled && nm.SelfNode.Valid() && nm.SelfNode.KeySignature().Len() == 0
		})
		alice.WaitForCondition(t, "unsigned peer", 10*time.Second, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 2
		})
		assert.Empty(t, peerSignature(alice.Netmap(), "phone"))

		status, apiBody := apiCall(t, client, apiKey, http.MethodGet, api, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, []any{bob.NodeIDString()}, field(t, apiBody, "unsignedNodeIds"))

		bobNode, ok := srv.State().GetNodeByNodeKey(bob.NodePrivateKey().Public())
		require.True(t, ok)

		info := tailcfg.TKASignInfo{
			NodePublic:     bob.NodePrivateKey().Public(),
			RotationPubkey: bobNode.NLKey().Verifier(),
		}
		bad := signNodeKey(t, info, key.NewNLPrivate())

		status, body := tkaDo(alice, "sign", tailcfg.TKASubmitSignatureRequest{
			Version:   tailcfg.CurrentCapabilityVersion,
			NodeKey:   alice.NodePrivateKey().Public(),
			Signature: bad,
		})
		assert.Equal(t, http.StatusBadRequest, status, "an untrusted key signs nothing: %s", body)

		status, body = tkaDo(alice, "sign", tailcfg.TKASubmitSignatureRequest{
			Version:   tailcfg.CurrentCapabilityVersion,
			NodeKey:   alice.NodePrivateKey().Public(),
			Signature: signNodeKey(t, info, signer),
		})
		require.Equal(t, http.StatusOK, status, string(body))

		bob.WaitForCondition(t, "signed", 10*time.Second, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.KeySignature().Len() > 0
		})
		alice.WaitForCondition(t, "signed peer", 10*time.Second, func(nm *netmap.NetworkMap) bool {
			return len(peerSignature(nm, "phone")) > 0
		})

		status, body = tkaDo(alice, "affected-sigs", tailcfg.TKASignaturesUsingKeyRequest{
			Version: tailcfg.CurrentCapabilityVersion,
			NodeKey: alice.NodePrivateKey().Public(),
			KeyID:   signer.KeyID(),
		})
		require.Equal(t, http.StatusOK, status, string(body))
		assert.Len(t, decodeJSON[tailcfg.TKASignaturesUsingKeyResponse](t, body).Signatures, 3)

		status, body = tkaDo(alice, "affected-sigs", tailcfg.TKASignaturesUsingKeyRequest{
			Version: tailcfg.CurrentCapabilityVersion,
			NodeKey: alice.NodePrivateKey().Public(),
			KeyID:   second.KeyID(),
		})
		require.Equal(t, http.StatusOK, status, string(body))
		assert.Empty(t, decodeJSON[tailcfg.TKASignaturesUsingKeyResponse](t, body).Signatures)
	})

	t.Run("an expired node re-signs its new key", func(t *testing.T) {
		oldKey := bob.NodePrivateKey().Public()

		bobNode, ok := srv.State().GetNodeByNodeKey(oldKey)
		require.True(t, ok)

		_, c, err := srv.State().SetNodeExpiry(bobNode.ID(), new(time.Now().Add(-time.Hour)))
		require.NoError(t, err)
		srv.App.Change(c)

		// The expired node logs in again as tailscaled would: the server
		// hands it its old signature, it makes a new key and re-signs.
		bob.Disconnect(t)
		require.NoError(t, bob.ReloginAndPoll(t.Context()))

		bob.WaitForCondition(t, "new key signed", 10*time.Second, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid() && nm.SelfNode.Key() != oldKey && nm.SelfNode.KeySignature().Len() > 0
		})

		nm := bob.Netmap()
		require.NoError(t, authority.NodeKeyAuthorized(nm.SelfNode.Key(), nm.SelfNode.KeySignature().AsSlice()),
			"the rotation signature chains back to the signing key")
		alice.WaitForCondition(t, "rotated peer", 10*time.Second, func(nm *netmap.NetworkMap) bool {
			for _, p := range nm.Peers {
				if p.Key() == bob.NodePrivateKey().Public() {
					return p.KeySignature().Len() > 0
				}
			}

			return false
		})
	})

	t.Run("the operator switches the lock off", func(t *testing.T) {
		status, body := tkaDo(alice, "disable", tailcfg.TKADisableRequest{
			Version:           tailcfg.CurrentCapabilityVersion,
			NodeKey:           alice.NodePrivateKey().Public(),
			DisablementSecret: bytes.Repeat([]byte{9}, 32),
		})
		assert.Equal(t, http.StatusForbidden, status, "a wrong secret changes nothing: %s", body)

		status, apiBody := apiCall(t, client, apiKey, http.MethodPost, api+"/disable", nil)
		require.Equal(t, http.StatusOK, status, apiBody)
		assert.False(t, boolField(t, apiBody, "enabled"))

		for _, node := range []*servertest.TestClient{alice, server, bob} {
			node.WaitForCondition(t, "lock off", 10*time.Second, func(nm *netmap.NetworkMap) bool {
				return !nm.TKAEnabled && nm.SelfNode.KeySignature().Len() == 0
			})
		}

		status, body = tkaDo(server, "bootstrap", tailcfg.TKABootstrapRequest{
			Version: tailcfg.CurrentCapabilityVersion,
			NodeKey: server.NodePrivateKey().Public(),
			Head:    authority.Head().String(),
		})
		require.Equal(t, http.StatusOK, status, string(body))

		resp := decodeJSON[tailcfg.TKABootstrapResponse](t, body)
		assert.Empty(t, resp.GenesisAUM)
		assert.True(t, authority.ValidDisablement(resp.DisablementSecret), "nodes get a secret their authority accepts")

		status, apiBody = apiCall(t, client, apiKey, http.MethodPost, api+"/disable", nil)
		assert.Equal(t, http.StatusConflict, status, "already off: %v", apiBody)
	})
}
