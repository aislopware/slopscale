package state

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/tka"
	"tailscale.com/types/key"
	"tailscale.com/types/tkatype"
)

// Tailnet lock on the server side; see docs/ref/tailnet-lock.md. The
// nodes hold the signing keys: one of them proposes the genesis and signs
// every node key, and later ones add and remove keys or sign new nodes.
// The server keeps the authority's log, verifies what it is handed, hands
// every map response the head so nodes sync, stamps each node key
// signature on the node for its peers, and hands a node whose key changed
// its old signature to re-sign.

// pendingLockInitTTL is how long a proposed genesis waits for its
// signatures before init/finish has to start over.
const pendingLockInitTTL = 10 * time.Minute

// ErrTailnetLockInvalid wraps a message the tka package cannot decode or
// verify.
var ErrTailnetLockInvalid = errors.New("invalid tailnet lock message")

// tailnetLock is the lock's state: the log with the authority open on it
// while the lock is on, the settings row, and the genesis a node proposed
// and has yet to finish. mu serialises every operation; info is the
// snapshot the mapper reads for each map response.
type tailnetLock struct {
	mu        sync.Mutex
	settings  types.TailnetLockSettings
	chonk     *tkaChonk
	authority *tka.Authority
	pending   map[key.NodePublic]pendingLockInit
	info      atomic.Pointer[tailcfg.TKAInfo]
}

// pendingLockInit is a genesis proposed by init/begin, with an authority
// opened on a scratch log to verify the signatures init/finish brings.
type pendingLockInit struct {
	genesis   tka.AUM
	authority *tka.Authority
	expires   time.Time
}

// tkaChonk is the [tka.Chonk] the authority reads and writes: a
// [tka.Mem] for the graph walks, with every mutation written through to
// the tka_aums table and the settings row, so the log survives a
// restart. The lock's mutex serialises its callers.
type tkaChonk struct {
	*tka.Mem

	lock *tailnetLock
	db   *hsdb.HSDatabase
}

var _ tka.CompactableChonk = (*tkaChonk)(nil)

// CommitVerifiedAUMs stores AUMs the authority verified.
func (c *tkaChonk) CommitVerifiedAUMs(updates []tka.AUM) error {
	err := c.Mem.CommitVerifiedAUMs(updates)
	if err != nil {
		return fmt.Errorf("committing AUMs: %w", err)
	}

	now := time.Now().UTC()
	rows := make([]types.TKAAUM, 0, len(updates))

	for i := range updates {
		row := types.TKAAUM{Hash: updates[i].Hash().String(), AUM: updates[i].Serialize(), CommittedAt: now}

		if parent, ok := updates[i].Parent(); ok {
			row.PrevHash = parent.String()
		}

		rows = append(rows, row)
	}

	return c.db.SaveTKAAUMs(rows)
}

// SetLastActiveAncestor records the oldest AUM the authority walks from.
func (c *tkaChonk) SetLastActiveAncestor(hash tka.AUMHash) error {
	err := c.Mem.SetLastActiveAncestor(hash)
	if err != nil {
		return fmt.Errorf("setting the last active ancestor: %w", err)
	}

	c.lock.settings.LastActiveAncestor = hash.String()

	return c.db.SaveTailnetLock(c.lock.settings)
}

// PurgeAUMs drops AUMs compaction no longer needs.
func (c *tkaChonk) PurgeAUMs(hashes []tka.AUMHash) error {
	err := c.Mem.PurgeAUMs(hashes)
	if err != nil {
		return fmt.Errorf("purging AUMs: %w", err)
	}

	encoded := make([]string, len(hashes))
	for i, h := range hashes {
		encoded[i] = h.String()
	}

	return c.db.DeleteTKAAUMs(encoded)
}

// RemoveAll empties the log, for when the lock is switched off.
func (c *tkaChonk) RemoveAll() error {
	err := c.Mem.RemoveAll()
	if err != nil {
		return fmt.Errorf("clearing the log: %w", err)
	}

	return c.db.ClearTKAAUMs()
}

// loadTailnetLock reads the log and the settings and opens the
// authority when the lock is on. It runs once at start.
func (s *State) loadTailnetLock() error {
	settings, err := s.db.LoadTailnetLock()
	if err != nil {
		return err
	}

	rows, err := s.db.LoadTKAAUMs()
	if err != nil {
		return err
	}

	mem := tka.ChonkMem()

	aums := make([]tka.AUM, 0, len(rows))

	for _, row := range rows {
		var aum tka.AUM

		err = aum.Unserialize(row.AUM)
		if err != nil {
			return fmt.Errorf("tailnet lock AUM %s: %w", row.Hash, err)
		}

		aums = append(aums, aum)
	}

	if len(aums) > 0 {
		err = mem.CommitVerifiedAUMs(aums)
		if err != nil {
			return fmt.Errorf("loading the tailnet lock log: %w", err)
		}
	}

	if settings.LastActiveAncestor != "" {
		var ancestor tka.AUMHash

		err = ancestor.UnmarshalText([]byte(settings.LastActiveAncestor))
		if err != nil {
			return fmt.Errorf("tailnet lock ancestor %q: %w", settings.LastActiveAncestor, err)
		}

		err = mem.SetLastActiveAncestor(ancestor)
		if err != nil {
			return fmt.Errorf("tailnet lock ancestor: %w", err)
		}
	}

	l := &tailnetLock{settings: settings, pending: map[key.NodePublic]pendingLockInit{}}
	l.chonk = &tkaChonk{Mem: mem, lock: l, db: s.db}

	if settings.Enabled {
		l.authority, err = tka.Open(l.chonk)
		if err != nil {
			return fmt.Errorf("opening the tailnet lock authority: %w", err)
		}
	}

	l.publish()
	s.tailnetLock = l

	return nil
}

// publish refreshes the snapshot the mapper reads. It runs under mu.
func (l *tailnetLock) publish() {
	if l.authority == nil {
		l.info.Store(&tailcfg.TKAInfo{Disabled: true})

		return
	}

	l.info.Store(&tailcfg.TKAInfo{Head: l.authority.Head().String()})
}

// TKAInfo is what every map response tells the node about tailnet lock:
// the head to sync to while the lock is on, or that it is off, which a
// node that still has the lock on takes as its cue to fetch the
// disablement secret.
func (s *State) TKAInfo() *tailcfg.TKAInfo {
	return s.tailnetLock.info.Load()
}

// TailnetLockEnabled reports whether the lock is on.
func (s *State) TailnetLockEnabled() bool {
	info := s.tailnetLock.info.Load()

	return info != nil && !info.Disabled
}

// TailnetLock is what the API and the console show about the lock.
func (s *State) TailnetLock() types.TailnetLockStatus {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	status := types.TailnetLockStatus{
		Enabled:                     l.authority != nil,
		Keys:                        []types.TailnetLockKey{},
		Signed:                      []types.NodeID{},
		Unsigned:                    []types.NodeID{},
		SupportDisablementAvailable: len(l.settings.SupportDisablement) > 0,
		EnabledAt:                   l.settings.EnabledAt,
		DisabledAt:                  l.settings.DisabledAt,
	}

	if l.authority == nil {
		return status
	}

	status.Head = l.authority.Head().String()

	for _, k := range l.authority.Keys() {
		status.Keys = append(status.Keys, types.TailnetLockKey{
			ID:     hex.EncodeToString(k.MustID()),
			Public: key.NLPublicFromEd25519Unsafe(k.Public).CLIString(),
			Votes:  k.Votes,
		})
	}

	for _, node := range s.ListNodes().All() {
		if node.KeySignature().Len() > 0 &&
			l.authority.NodeKeyAuthorized(node.NodeKey(), node.KeySignature().AsSlice()) == nil {
			status.Signed = append(status.Signed, node.ID())
		} else {
			status.Unsigned = append(status.Unsigned, node.ID())
		}
	}

	return status
}

// TailnetLockInitBegin takes the genesis a node proposes, checks it
// stands on its own and answers with every node that needs a signature
// before the lock can be switched on. Only a node owned by an owner or
// admin may propose one, as on the hosted control plane.
func (s *State) TailnetLockInitBegin(
	node types.NodeView,
	genesis tkatype.MarshaledAUM,
) ([]tailcfg.TKASignInfo, error) {
	err := s.tailnetLockInitAllowed(node)
	if err != nil {
		return nil, err
	}

	var aum tka.AUM

	err = aum.Unserialize(genesis)
	if err != nil {
		return nil, fmt.Errorf("%w: genesis: %w", ErrTailnetLockInvalid, err)
	}

	authority, err := tka.Bootstrap(tka.ChonkMem(), aum)
	if err != nil {
		return nil, fmt.Errorf("%w: genesis: %w", ErrTailnetLockInvalid, err)
	}

	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority != nil {
		return nil, types.ErrTailnetLockEnabled
	}

	now := time.Now()

	for nodeKey, pending := range l.pending {
		if pending.expires.Before(now) {
			delete(l.pending, nodeKey)
		}
	}

	l.pending[node.NodeKey()] = pendingLockInit{
		genesis:   aum,
		authority: authority,
		expires:   now.Add(pendingLockInitTTL),
	}

	nodes := s.ListNodes()
	need := make([]tailcfg.TKASignInfo, 0, nodes.Len())

	for _, n := range nodes.All() {
		info := tailcfg.TKASignInfo{NodeID: n.ID().NodeID(), NodePublic: n.NodeKey()}

		if !n.NLKey().IsZero() {
			info.RotationPubkey = n.NLKey().Verifier()
		}

		need = append(need, info)
	}

	return need, nil
}

// tailnetLockInitAllowed checks the proposing node belongs to an owner
// or admin; the policy manager, not the node's user copy, is the source
// of roles, so the user is read afresh.
func (s *State) tailnetLockInitAllowed(node types.NodeView) error {
	if node.IsTagged() || !node.UserID().Valid() {
		return types.ErrTailnetLockNotAdmin
	}

	user, err := s.GetUserByID(types.UserID(node.UserID().Get()))
	if err != nil {
		return fmt.Errorf("reading the node's user: %w", err)
	}

	if !user.Role.IsAdmin() {
		return types.ErrTailnetLockNotAdmin
	}

	return nil
}

// TailnetLockInitFinish switches the lock on with the genesis the node
// proposed, once every node has a signature the genesis authority
// accepts. supportDisablement is the secret the client minted for the
// operator, which the API's disable uses.
func (s *State) TailnetLockInitFinish(
	node types.NodeView,
	signatures map[tailcfg.NodeID]tkatype.MarshaledSignature,
	supportDisablement []byte,
) (change.Change, error) {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority != nil {
		return change.Change{}, types.ErrTailnetLockEnabled
	}

	pending, ok := l.pending[node.NodeKey()]
	if !ok || pending.expires.Before(time.Now()) {
		delete(l.pending, node.NodeKey())

		return change.Change{}, types.ErrTailnetLockNoPendingInit
	}

	verified, err := verifyLockSignatures(pending.authority, s.ListNodes().All(), signatures)
	if err != nil {
		return change.Change{}, err
	}

	// A log left behind by a failure between the commit and the switch
	// would refuse the bootstrap.
	err = l.chonk.RemoveAll()
	if err != nil {
		return change.Change{}, fmt.Errorf("clearing the tailnet lock log: %w", err)
	}

	authority, err := tka.Bootstrap(l.chonk, pending.genesis)
	if err != nil {
		return change.Change{}, fmt.Errorf("storing the genesis: %w", err)
	}

	now := time.Now().UTC()
	l.settings.Enabled = true
	l.settings.Genesis = pending.genesis.Hash().String()
	l.settings.SupportDisablement = supportDisablement
	l.settings.DisablementSecret = nil
	l.settings.EnabledAt = &now
	l.settings.DisabledAt = nil

	err = s.db.SaveTailnetLock(l.settings)
	if err != nil {
		return change.Change{}, err
	}

	l.authority = authority
	delete(l.pending, node.NodeKey())

	err = s.storeKeySignatures(verified)
	if err != nil {
		return change.Change{}, err
	}

	l.publish()

	log.Info().Caller().
		Uint64("node.id", node.ID().Uint64()).
		Str("tka.head", authority.Head().String()).
		Int("signed", len(verified)).
		Msg("tailnet lock enabled")

	return tailnetLockChange("tailnet lock enabled"), nil
}

// verifyLockSignatures checks that signatures carries one the authority
// accepts for every node.
func verifyLockSignatures(
	authority *tka.Authority,
	nodes func(func(int, types.NodeView) bool),
	signatures map[tailcfg.NodeID]tkatype.MarshaledSignature,
) (map[types.NodeID]tkatype.MarshaledSignature, error) {
	verified := make(map[types.NodeID]tkatype.MarshaledSignature, len(signatures))

	for _, n := range nodes {
		sig, ok := signatures[n.ID().NodeID()]
		if !ok || len(sig) == 0 {
			return nil, fmt.Errorf("%w: node %d (%s)", types.ErrTailnetLockMissingSignature, n.ID(), n.GivenName())
		}

		err := authority.NodeKeyAuthorized(n.NodeKey(), sig)
		if err != nil {
			return nil, fmt.Errorf("%w: node %d (%s): %w", types.ErrTailnetLockBadSignature, n.ID(), n.GivenName(), err)
		}

		verified[n.ID()] = sig
	}

	return verified, nil
}

// storeKeySignatures records the signatures on the nodes; a nil one
// clears the node's.
func (s *State) storeKeySignatures(signatures map[types.NodeID]tkatype.MarshaledSignature) error {
	updates := make(map[types.NodeID]UpdateNodeFunc, len(signatures))

	for nodeID, sig := range signatures {
		updates[nodeID] = func(node *types.Node) { node.KeySignature = sig }
	}

	s.nodeStore.UpdateNodes(updates)

	// persistNodeToDB writes the column too, but from whichever snapshot
	// the caller holds; this write carries exactly these values.
	err := s.db.NodeSetKeySignatures(signatures)
	if err != nil {
		return fmt.Errorf("storing node key signatures: %w", err)
	}

	return nil
}

// clearKeySignatures drops every node's signature, for when the lock is
// switched off.
func (s *State) clearKeySignatures() error {
	updates := map[types.NodeID]UpdateNodeFunc{}

	for _, node := range s.ListNodes().All() {
		if node.KeySignature().Len() > 0 {
			updates[node.ID()] = func(node *types.Node) { node.KeySignature = nil }
		}
	}

	if len(updates) > 0 {
		s.nodeStore.UpdateNodes(updates)
	}

	return s.db.NodeClearKeySignatures()
}

// tailnetLockChange is what a change to the signatures needs: the
// signature rides on the self node and on every peer entry, so every node
// gets both again.
func tailnetLockChange(reason string) change.Change {
	c := change.FullUpdate()
	c.Reason = reason

	return c
}

// TailnetLockBootstrap is what a node needs to follow the lock's state:
// the genesis while the lock is on, so it can build the authority, or
// the secret the lock was switched off with, so it can switch off too.
func (s *State) TailnetLockBootstrap() (tailcfg.TKABootstrapResponse, error) {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority == nil {
		return tailcfg.TKABootstrapResponse{DisablementSecret: l.settings.DisablementSecret}, nil
	}

	var genesisHash tka.AUMHash

	err := genesisHash.UnmarshalText([]byte(l.settings.Genesis))
	if err != nil {
		return tailcfg.TKABootstrapResponse{}, fmt.Errorf("stored genesis hash %q: %w", l.settings.Genesis, err)
	}

	genesis, err := l.chonk.AUM(genesisHash)
	if err != nil {
		return tailcfg.TKABootstrapResponse{}, fmt.Errorf("reading the genesis: %w", err)
	}

	return tailcfg.TKABootstrapResponse{GenesisAUM: genesis.Serialize()}, nil
}

// TailnetLockSyncOffer answers a node's sync offer with the server's own
// and the AUMs the node is missing.
func (s *State) TailnetLockSyncOffer(head string, ancestors []string) (tailcfg.TKASyncOfferResponse, error) {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority == nil {
		return tailcfg.TKASyncOfferResponse{}, types.ErrTailnetLockDisabled
	}

	nodeOffer, err := tka.ToSyncOffer(head, ancestors)
	if err != nil {
		return tailcfg.TKASyncOfferResponse{}, fmt.Errorf("%w: sync offer: %w", ErrTailnetLockInvalid, err)
	}

	serverOffer, err := l.authority.SyncOffer(l.chonk)
	if err != nil {
		return tailcfg.TKASyncOfferResponse{}, fmt.Errorf("computing the sync offer: %w", err)
	}

	missing, err := l.authority.MissingAUMs(l.chonk, nodeOffer)
	if err != nil {
		return tailcfg.TKASyncOfferResponse{}, fmt.Errorf("computing the missing AUMs: %w", err)
	}

	serverHead, serverAncestors, err := tka.FromSyncOffer(serverOffer)
	if err != nil {
		return tailcfg.TKASyncOfferResponse{}, fmt.Errorf("encoding the sync offer: %w", err)
	}

	resp := tailcfg.TKASyncOfferResponse{
		Head:        serverHead,
		Ancestors:   serverAncestors,
		MissingAUMs: make([]tkatype.MarshaledAUM, 0, len(missing)),
	}

	for i := range missing {
		resp.MissingAUMs = append(resp.MissingAUMs, missing[i].Serialize())
	}

	return resp, nil
}

// TailnetLockSyncSend takes the AUMs a node believes the server is
// missing and returns the head afterwards. A moved head is broadcast so
// every node syncs.
func (s *State) TailnetLockSyncSend(aums []tkatype.MarshaledAUM) (string, change.Change, error) {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority == nil {
		return "", change.Change{}, types.ErrTailnetLockDisabled
	}

	before := l.authority.Head()

	if len(aums) > 0 {
		updates := make([]tka.AUM, len(aums))

		for i, raw := range aums {
			err := updates[i].Unserialize(raw)
			if err != nil {
				return "", change.Change{}, fmt.Errorf("%w: AUM %d: %w", ErrTailnetLockInvalid, i, err)
			}
		}

		err := l.authority.Inform(l.chonk, updates)
		if err != nil {
			return "", change.Change{}, fmt.Errorf("%w: %w", ErrTailnetLockInvalid, err)
		}
	}

	head := l.authority.Head()
	if head == before {
		return head.String(), change.Change{}, nil
	}

	l.publish()

	log.Info().Caller().
		Str("tka.head", head.String()).
		Int("aums", len(aums)).
		Msg("tailnet lock head moved")

	// Only the head, on the self node, changed.
	c := change.PolicyChange()
	c.Reason = "tailnet lock head"
	c.IncludeSelf = true

	return head.String(), c, nil
}

// TailnetLockDisable switches the lock off with a disablement secret a
// node holds, as `tailscale lock disable` does.
func (s *State) TailnetLockDisable(secret []byte) (change.Change, error) {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority == nil {
		return change.Change{}, types.ErrTailnetLockDisabled
	}

	if !l.authority.ValidDisablement(secret) {
		return change.Change{}, types.ErrTailnetLockBadSecret
	}

	return s.disableTailnetLockLocked(l, secret)
}

// DisableTailnetLock switches the lock off with the support disablement
// secret the initialising client minted, for the API and the console.
func (s *State) DisableTailnetLock() (change.Change, error) {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority == nil {
		return change.Change{}, types.ErrTailnetLockDisabled
	}

	if len(l.settings.SupportDisablement) == 0 {
		return change.Change{}, types.ErrTailnetLockNoSupportSecret
	}

	if !l.authority.ValidDisablement(l.settings.SupportDisablement) {
		return change.Change{}, types.ErrTailnetLockBadSecret
	}

	return s.disableTailnetLockLocked(l, l.settings.SupportDisablement)
}

// disableTailnetLockLocked empties the log, keeps the secret for nodes
// that still have the lock on and drops every signature. It runs under mu.
func (s *State) disableTailnetLockLocked(l *tailnetLock, secret []byte) (change.Change, error) {
	err := l.chonk.RemoveAll()
	if err != nil {
		return change.Change{}, fmt.Errorf("clearing the tailnet lock log: %w", err)
	}

	now := time.Now().UTC()
	l.settings = types.TailnetLockSettings{
		DisablementSecret: secret,
		EnabledAt:         l.settings.EnabledAt,
		DisabledAt:        &now,
	}

	err = s.db.SaveTailnetLock(l.settings)
	if err != nil {
		return change.Change{}, err
	}

	l.authority = nil
	clear(l.pending)

	err = s.clearKeySignatures()
	if err != nil {
		return change.Change{}, err
	}

	l.publish()

	log.Info().Caller().Msg("tailnet lock disabled")

	return tailnetLockChange("tailnet lock disabled"), nil
}

// TailnetLockSubmitSignature records a signature a signing node made
// for another node's key, as `tailscale lock sign` does, and returns the
// node it is for.
func (s *State) TailnetLockSubmitSignature(sig tkatype.MarshaledSignature) (types.NodeView, change.Change, error) {
	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority == nil {
		return types.NodeView{}, change.Change{}, types.ErrTailnetLockDisabled
	}

	var decoded tka.NodeKeySignature

	err := decoded.Unserialize(sig)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: signature: %w", ErrTailnetLockInvalid, err)
	}

	var nodeKey key.NodePublic

	err = nodeKey.UnmarshalBinary(decoded.Pubkey)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: signed key: %w", ErrTailnetLockInvalid, err)
	}

	node, ok := s.GetNodeByNodeKey(nodeKey)
	if !ok {
		return types.NodeView{}, change.Change{}, ErrNodeNotFound
	}

	err = l.authority.NodeKeyAuthorized(nodeKey, sig)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %w", types.ErrTailnetLockBadSignature, err)
	}

	err = s.storeKeySignatures(map[types.NodeID]tkatype.MarshaledSignature{node.ID(): sig})
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	log.Info().Caller().
		Uint64("node.id", node.ID().Uint64()).
		Str("node.name", node.GivenName()).
		Msg("tailnet lock node key signature recorded")

	// The node gets its self node with the signature and its peers the
	// peer entry with it.
	c := change.NodeAdded(node.ID())
	c.Reason = "tailnet lock signature"

	return node, c, nil
}

// TailnetLockAffectedSignatures lists the stored node key signatures the
// given signing key authorised, which a node re-signs before removing
// the key.
func (s *State) TailnetLockAffectedSignatures(keyID tkatype.KeyID) []tkatype.MarshaledSignature {
	out := []tkatype.MarshaledSignature{}

	for _, node := range s.ListNodes().All() {
		sig := node.KeySignature().AsSlice()
		if len(sig) == 0 {
			continue
		}

		var decoded tka.NodeKeySignature

		err := decoded.Unserialize(sig)
		if err != nil {
			continue
		}

		signer, err := decoded.UnverifiedAuthorizingKeyID()
		if err != nil {
			continue
		}

		if bytes.Equal(signer, keyID) {
			out = append(out, sig)
		}
	}

	return out
}

// tailnetLockSignature is the signature to store for a node registering
// with nodeKey: the one the client sent, when the lock is on and the
// authority accepts it; nothing otherwise. A signature that does not
// verify is logged and leaves the node unsigned, which locks it out until
// a signing node signs it, rather than refusing the login.
func (s *State) tailnetLockSignature(
	nodeKey key.NodePublic,
	hostname string,
	sig tkatype.MarshaledSignature,
) tkatype.MarshaledSignature {
	if len(sig) == 0 {
		return nil
	}

	l := s.tailnetLock

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.authority == nil {
		return nil
	}

	err := l.authority.NodeKeyAuthorized(nodeKey, sig)
	if err != nil {
		log.Warn().Caller().
			Err(err).
			Str("node.key", nodeKey.ShortString()).
			Str("node.hostname", hostname).
			Msg("registration carried a node key signature the tailnet lock authority rejects; the node stays unsigned")

		return nil
	}

	return sig
}

// TailnetLockRotation is the stored signature a machine must re-sign for
// a new node key: when the lock is on, the register request carries no
// signature and the machine's node holds a signature for another key or
// its key expired, the client gets the old signature back
// ([tailcfg.RegisterResponse.NodeKeySignature]) and registers again with
// a new key and a rotation signature. Nil means carry on.
func (s *State) TailnetLockRotation(
	machineKey key.MachinePublic,
	nodeKey key.NodePublic,
	sig tkatype.MarshaledSignature,
) tkatype.MarshaledSignature {
	if len(sig) > 0 || !s.TailnetLockEnabled() {
		return nil
	}

	for _, node := range s.GetNodesByMachineKeyAllUsers(machineKey) {
		stored := node.KeySignature().AsSlice()
		if len(stored) == 0 {
			continue
		}

		if node.NodeKey() != nodeKey || node.IsExpired() {
			return stored
		}
	}

	return nil
}
