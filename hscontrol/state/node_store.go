package state

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
	"tailscale.com/util/dnsname"
)

// fallbackGivenName is the DNS label used when a node is written with
// an empty [types.Node.GivenName]. Matches Tailscale SaaS behaviour
// for empty sanitised labels.
const fallbackGivenName = "node"

// Errors returned by [NodeStore.SetGivenName]. [ErrNodeNotFound] is defined
// in state.go and reused here.
var (
	ErrGivenNameTaken   = errors.New("given name already in use by another node")
	ErrGivenNameInvalid = errors.New("given name is not a valid DNS label")
)

const (
	put             = 1
	del             = 2
	rebuildPeerMaps = 4
	setName         = 5
	updateMulti     = 6
)

const prometheusNamespace = "headscale"

var (
	nodeStoreOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_operations_total",
		Help:      "Total number of NodeStore operations",
	}, []string{"operation"})
	nodeStoreOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_operation_duration_seconds",
		Help:      "Duration of NodeStore operations",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation"})
	nodeStoreBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_batch_size",
		Help:      "Size of NodeStore write batches",
		Buckets:   []float64{1, 2, 5, 10, 20, 50, 100},
	})
	nodeStoreBatchDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_batch_duration_seconds",
		Help:      "Duration of NodeStore batch processing",
		Buckets:   prometheus.DefBuckets,
	})
	nodeStoreSnapshotBuildDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_snapshot_build_duration_seconds",
		Help:      "Duration of NodeStore snapshot building from nodes",
		Buckets:   prometheus.DefBuckets,
	})
	nodeStoreNodesCount = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_nodes",
		Help:      "Number of nodes in the NodeStore",
	})
	nodeStorePeersCalculationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_peers_calculation_duration_seconds",
		Help:      "Duration of peers calculation in NodeStore",
		Buckets:   prometheus.DefBuckets,
	})
	nodeStoreQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "nodestore_queue_depth",
		Help:      "Current depth of NodeStore write queue",
	})
)

// NodeStore is a thread-safe store for nodes.
// It is a copy-on-write structure, replacing the "snapshot"
// when a change to the structure occurs. It is optimised for reads,
// and while batches are not fast, they are grouped together
// to do less of the expensive peer calculation if there are many
// changes rapidly.
//
// Writes will block until committed, while reads are never
// blocked. This means that the caller of a write operation
// is responsible for ensuring an update depending on a write
// is not issued before the write is complete.
type NodeStore struct {
	data atomic.Pointer[Snapshot]

	peersFunc  PeerPositionsFunc
	writeQueue chan work

	// stopped is closed once by Stop to signal the writer goroutine to exit
	// and to let in-flight writes return cleanly instead of panicking with
	// "send on closed channel" during shutdown.
	stopped  chan struct{}
	stopOnce sync.Once

	batchSize    int
	batchTimeout time.Duration
}

// NewNodeStore builds a store whose peers come from a [PeersFunc] keyed by
// node id. Production passes positions through [NewNodeStorePositional];
// this constructor adapts the map form so tests can hand in any peer
// relationship.
func NewNodeStore(allNodes types.Nodes, peersFunc PeersFunc, batchSize int, batchTimeout time.Duration) *NodeStore {
	return NewNodeStorePositional(allNodes, peerPositionsOf(peersFunc), batchSize, batchTimeout)
}

// NewNodeStorePositional builds a store whose peers come from a
// [PeerPositionsFunc].
func NewNodeStorePositional(
	allNodes types.Nodes,
	peersFunc PeerPositionsFunc,
	batchSize int,
	batchTimeout time.Duration,
) *NodeStore {
	nodes := make(map[types.NodeID]types.Node, len(allNodes))
	for _, n := range allNodes {
		nodes[n.ID] = *n
	}

	snap := snapshotFromNodes(nodes, peersFunc, PrimaryRouteLedger{}, nil)

	store := &NodeStore{
		peersFunc:    peersFunc,
		batchSize:    batchSize,
		batchTimeout: batchTimeout,
		stopped:      make(chan struct{}),
	}
	store.data.Store(&snap)

	// Initialize node count gauge
	nodeStoreNodesCount.Set(float64(len(nodes)))

	return store
}

// operationMetrics is the duration histogram and counter of one NodeStore
// operation with its label resolved once, so the reads on the map request
// path pay an atomic add and an observe rather than a label lookup and a
// timer allocation per call.
type operationMetrics struct {
	duration prometheus.Observer
	count    prometheus.Counter
}

func newOperationMetrics(op string) operationMetrics {
	return operationMetrics{
		duration: nodeStoreOperationDuration.WithLabelValues(op),
		count:    nodeStoreOperations.WithLabelValues(op),
	}
}

// observe records one operation that started at start; call it deferred
// with time.Now() as the argument.
func (m operationMetrics) observe(start time.Time) {
	m.duration.Observe(time.Since(start).Seconds())
	m.count.Inc()
}

// observeDuration records only the duration of one operation. The write
// paths count the operation separately, once the write has completed, so
// a write that is dropped by a stopped store is not counted.
func (m operationMetrics) observeDuration(start time.Time) {
	m.duration.Observe(time.Since(start).Seconds())
}

// observeSince records the time since start on an unlabelled histogram.
// Used instead of [prometheus.NewTimer], which allocates a timer per call.
func observeSince(h prometheus.Observer, start time.Time) {
	h.Observe(time.Since(start).Seconds())
}

var (
	nodeStoreGetMetrics                          = newOperationMetrics("get")
	nodeStoreGetByKeyMetrics                     = newOperationMetrics("get_by_key")
	nodeStoreGetNodesByMachineKeyAllUsersMetrics = newOperationMetrics("get_nodes_by_machine_key_all_users")
	nodeStoreListMetrics                         = newOperationMetrics("list")
	nodeStoreListByUserMetrics                   = newOperationMetrics("list_by_user")
	nodeStoreListPeersMetrics                    = newOperationMetrics("list_peers")
	nodeStorePutMetrics                          = newOperationMetrics("put")
	nodeStoreUpdateMetrics                       = newOperationMetrics("update")
	nodeStoreUpdateMultiMetrics                  = newOperationMetrics("update_multi")
	nodeStoreDeleteMetrics                       = newOperationMetrics("delete")
	nodeStoreSetNameMetrics                      = newOperationMetrics("set_name")
)

// Snapshot is the representation of the current state of the [NodeStore].
// It contains all nodes and their relationships.
// It is a copy-on-write structure, meaning that when a write occurs,
// a new [Snapshot] is created with the updated state,
// and replaces the old one atomically.
type Snapshot struct {
	// nodesByID is the main source of truth for nodes.
	nodesByID map[types.NodeID]types.Node

	// calculated from nodesByID
	// nodeViewsByID holds one view per node over a copy the snapshot
	// owns, so a read hands out a pointer instead of copying the node.
	nodeViewsByID     map[types.NodeID]types.NodeView
	nodesByNodeKey    map[key.NodePublic]types.NodeView
	nodesByMachineKey map[key.MachinePublic]machineKeyNodes
	nodesByUser       map[types.UserID][]types.NodeView
	// allNodes is sorted by id, so peerPositions stays valid across
	// batches that keep the node set.
	allNodes []types.NodeView
	// posByID is each node's position in allNodes.
	posByID map[types.NodeID]int32

	// peerPositions is the peer relationship as positions into allNodes,
	// the form the policy computes it in: peerPositions[i] lists the
	// positions of node i's peers, nil for a node without an entry. A
	// batch that changes no input of that computation carries it forward
	// instead of paying the O(n²) policy scan, and a read resolves the
	// views through allNodes, so the peers always point at the current
	// copies.
	peerPositions [][]int32

	// routes maps each prefix to its current primary advertiser. The
	// previous assignment is carried over when still valid so the
	// primary does not flap on every unrelated batch.
	routes         map[netip.Prefix]types.NodeID
	isPrimaryRoute map[types.NodeID]bool

	// regionalRoutes maps a DERP region to the primary advertiser of each
	// prefix among the region's own online, healthy advertisers. A viewer
	// homed in that region is steered to it instead of the tailnet-wide
	// primary, as Tailscale's regional routing does; a region without a
	// healthy advertiser for a prefix has no entry and falls back.
	regionalRoutes map[tailcfg.DERPRegionID]map[netip.Prefix]types.NodeID
}

// machineKeyNode is one node of the machine key index together with the
// user that owns it. Tagged nodes carry the tagged sentinel UserID(0).
type machineKeyNode struct {
	userID types.UserID
	node   types.NodeView
}

// machineKeyNodes holds every node registered under one machine key. A
// device is normally registered by a single user, so the first node is
// stored inline and rest stays nil; only a device registered by several
// users (the "create new, do not transfer" path) allocates. Keeping the
// common case allocation-free matters because the index is rebuilt for
// every node on every write batch.
type machineKeyNodes struct {
	first machineKeyNode
	rest  []machineKeyNode
}

// add records that node, owned by userID, uses the machine key this entry
// indexes.
func (m machineKeyNodes) add(userID types.UserID, node types.NodeView) machineKeyNodes {
	if !m.first.node.Valid() {
		m.first = machineKeyNode{userID: userID, node: node}
		return m
	}

	m.rest = append(m.rest, machineKeyNode{userID: userID, node: node})

	return m
}

// PrimaryRouteLedger is a snapshot's primary assignment: the tailnet-wide
// primary per prefix and the per-region primaries. Callers compare a
// ledger taken before a write with the one after to learn whether any
// viewer's routes moved.
type PrimaryRouteLedger struct {
	Global   map[netip.Prefix]types.NodeID
	Regional map[tailcfg.DERPRegionID]map[netip.Prefix]types.NodeID
}

// Equal reports whether both ledgers assign every prefix, tailnet-wide and
// per region, to the same node.
func (l PrimaryRouteLedger) Equal(o PrimaryRouteLedger) bool {
	return maps.Equal(l.Global, o.Global) &&
		maps.EqualFunc(l.Regional, o.Regional, maps.Equal)
}

// PeersFunc is a function that takes a list of nodes and returns a map
// with the relationships between nodes and their peers.
// This will typically be used to calculate which nodes can see each other
// based on the current policy.
type PeersFunc func(nodes []types.NodeView) map[types.NodeID][]types.NodeView

// PeerPositionsFunc computes the peer relationship as positions into the
// slice it is given: out[i] lists the positions of node i's peers. A nil
// list means the node has no entry in the peer map; an empty one means
// it has an entry without peers.
type PeerPositionsFunc func(nodes []types.NodeView) [][]int32

// peerPositionsOf adapts a [PeersFunc] to positions.
func peerPositionsOf(peersFunc PeersFunc) PeerPositionsFunc {
	return func(nodes []types.NodeView) [][]int32 {
		byID := peersFunc(nodes)
		if byID == nil {
			return nil
		}

		posByID := make(map[types.NodeID]int32, len(nodes))
		for i, n := range nodes {
			posByID[n.ID()] = int32(i)
		}

		out := make([][]int32, len(nodes))

		for i, n := range nodes {
			peers, ok := byID[n.ID()]
			if !ok {
				continue
			}

			list := make([]int32, 0, len(peers))
			for _, p := range peers {
				if pos, ok := posByID[p.ID()]; ok {
					list = append(list, pos)
				}
			}

			out[i] = list
		}

		return out
	}
}

// peerInputsChanged reports whether an update to a node could change who
// its peers are: the inputs the policy reads (what HasPolicyChange
// compares), whether the node is admitted, its routes and exit status,
// and the user identity the policy resolves names through. A batch of
// updates that changes none of these keeps the previous peer map.
func peerInputsChanged(old, updated *types.Node) bool {
	if old.View().HasPolicyChange(updated.View()) {
		return true
	}

	if old.IsAdmitted() != updated.IsAdmitted() || old.IsExitNode() != updated.IsExitNode() {
		return true
	}

	if !slices.Equal(old.ApprovedRoutes, updated.ApprovedRoutes) ||
		!slices.Equal(old.AnnouncedRoutes(), updated.AnnouncedRoutes()) {
		return true
	}

	return !userIdentityEqual(old.User, updated.User)
}

// userIdentityEqual compares what the policy resolves a user by. A tagged
// node carries no user.
func userIdentityEqual(a, b *types.User) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Name == b.Name &&
		a.Email == b.Email &&
		a.Role == b.Role &&
		a.ProviderIdentifier == b.ProviderIdentifier
}

// work represents a single operation to be performed on the [NodeStore].
type work struct {
	op         int
	nodeID     types.NodeID
	node       types.Node
	result     chan struct{}
	nodeResult chan types.NodeView
	// For rebuildPeerMaps operation
	rebuildResult chan struct{}
	// For setName operation (admin rename, reject-on-collision path).
	name      string
	errResult chan error
	// For updateMulti: per-node update functions applied as a single
	// batch entry so callers that need an atomic election (e.g. the HA
	// prober applying multiple probe results at once) cannot have a
	// partial snapshot published between the updates.
	multiUpdates map[types.NodeID]UpdateNodeFunc
}

// PutNode adds or updates a node in the store.
// If the node already exists, it will be replaced.
// If the node does not exist, it will be added.
// This is a blocking operation that waits for the write to complete.
// Returns the resulting node after all modifications in the batch have been applied.
func (s *NodeStore) PutNode(n types.Node) types.NodeView {
	defer nodeStorePutMetrics.observeDuration(time.Now())

	w := work{
		op:         put,
		nodeID:     n.ID,
		node:       n,
		result:     make(chan struct{}),
		nodeResult: make(chan types.NodeView, 1),
	}

	nodeStoreQueueDepth.Inc()

	select {
	case s.writeQueue <- w:
	case <-s.stopped:
		nodeStoreQueueDepth.Dec()

		return types.NodeView{}
	}

	<-w.result
	nodeStoreQueueDepth.Dec()

	resultNode := <-w.nodeResult

	nodeStorePutMetrics.count.Inc()

	return resultNode
}

// UpdateNodeFunc is a function type that takes a pointer to a [types.Node] and modifies it.
type UpdateNodeFunc func(n *types.Node)

// UpdateNode applies a function to modify a specific node in the
// store. Single-node convenience wrapper around [NodeStore.UpdateNodes]
// — the writer goroutine signals completion only after the post-batch
// snapshot has been stored, so the follow-up [NodeStore.GetNode] read
// sees the applied update. Returns the resulting node and whether it
// exists.
//
// Callers that need to change several nodes atomically should call
// [NodeStore.UpdateNodes] directly; collecting changes into one batch
// keeps the election from running on a half-applied snapshot.
func (s *NodeStore) UpdateNode(nodeID types.NodeID, updateFn UpdateNodeFunc) (types.NodeView, bool) {
	defer nodeStoreUpdateMetrics.observeDuration(time.Now())

	// Goes through updateNodes rather than UpdateNodes so a single-node
	// write is timed and counted once, as "update", not also as
	// "update_multi".
	if s.updateNodes(map[types.NodeID]UpdateNodeFunc{nodeID: updateFn}) {
		nodeStoreUpdateMetrics.count.Inc()
	}

	return s.GetNode(nodeID)
}

// UpdateNodes applies per-node update functions in a single atomic
// batch. The election that recomputes primary routes runs once, after
// every update has landed, so callers cannot observe an intermediate
// snapshot where only some of the updates are visible. Use this when
// the order in which two writers' updates are individually published
// would change the election outcome — e.g. the HA prober applying
// concurrent probe-timeout results.
func (s *NodeStore) UpdateNodes(updates map[types.NodeID]UpdateNodeFunc) {
	defer nodeStoreUpdateMultiMetrics.observeDuration(time.Now())

	if s.updateNodes(updates) {
		nodeStoreUpdateMultiMetrics.count.Inc()
	}
}

// DeleteNode removes a node from the store by its ID.
// This is a blocking operation that waits for the write to complete.
func (s *NodeStore) DeleteNode(id types.NodeID) {
	defer nodeStoreDeleteMetrics.observeDuration(time.Now())

	w := work{
		op:     del,
		nodeID: id,
		result: make(chan struct{}),
	}

	nodeStoreQueueDepth.Inc()

	select {
	case s.writeQueue <- w:
	case <-s.stopped:
		nodeStoreQueueDepth.Dec()

		return
	}

	<-w.result
	nodeStoreQueueDepth.Dec()

	nodeStoreDeleteMetrics.count.Inc()
}

// SetGivenName sets [types.Node.GivenName] on the node identified by id,
// rejecting the write if the name is already held by another node.
// Intended for the admin rename path, where auto-bumping a
// user-supplied name would be surprising.
//
// Returns:
//   - the stored [types.NodeView] and nil on success
//   - [ErrGivenNameInvalid]   if name is not a valid DNS label
//   - [ErrGivenNameTaken]     if another node already holds name
//   - [ErrNodeNotFound]       if no node with id exists
//
// Runs as a single writer-goroutine op, so the uniqueness check and the
// write are atomic with respect to concurrent
// [NodeStore.PutNode]/[NodeStore.UpdateNode].
func (s *NodeStore) SetGivenName(id types.NodeID, name string) (types.NodeView, error) {
	defer nodeStoreSetNameMetrics.observeDuration(time.Now())

	w := work{
		op:         setName,
		nodeID:     id,
		name:       name,
		result:     make(chan struct{}),
		nodeResult: make(chan types.NodeView, 1),
		errResult:  make(chan error, 1),
	}

	nodeStoreQueueDepth.Inc()

	select {
	case s.writeQueue <- w:
	case <-s.stopped:
		nodeStoreQueueDepth.Dec()

		return types.NodeView{}, nil
	}

	<-w.result
	nodeStoreQueueDepth.Dec()

	nodeStoreSetNameMetrics.count.Inc()

	err := <-w.errResult
	if err != nil {
		return types.NodeView{}, err
	}

	return <-w.nodeResult, nil
}

// Start initializes the [NodeStore] and starts processing the write queue.
func (s *NodeStore) Start() {
	s.writeQueue = make(chan work)
	go s.processWrite()
}

// Stop stops the [NodeStore]. It signals the writer goroutine via stopped
// rather than closing writeQueue, so writes racing shutdown drop cleanly
// instead of panicking on a closed channel.
func (s *NodeStore) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopped)
	})
}

// givenNames counts how many nodes hold each given name.
// [NodeStore.applyBatch] builds it once per batch and keeps it in step
// with the names the batch writes, so resolving a name costs a lookup per
// candidate instead of a scan over every node per operation.
type givenNames map[string]int

func newGivenNames(nodes map[types.NodeID]types.Node) givenNames {
	names := make(givenNames, len(nodes))
	for _, n := range nodes {
		names[n.GivenName]++
	}

	return names
}

// free reports whether candidate is available to the node currently named
// selfName: nobody holds it, or only that node does.
func (g givenNames) free(candidate, selfName string) bool {
	held := g[candidate]
	if candidate == selfName {
		held--
	}

	return held <= 0
}

// hold records one more node holding name.
func (g givenNames) hold(name string) {
	g[name]++
}

// release records one fewer node holding name.
func (g givenNames) release(name string) {
	if g[name] <= 1 {
		delete(g, name)
		return
	}

	g[name]--
}

// resolveGivenName returns a unique DNS label for a node whose current
// label is selfName (empty for a node that is being created), based on
// the caller-supplied base label. If base is empty it falls back to
// [fallbackGivenName] ("node"). The label's own holder is excluded from
// the collision check so an idempotent write keeps the current label.
//
// On collision the label is bumped as base, base-1, base-2, …, first
// unused wins. Must be called from the [NodeStore] writer goroutine
// (inside [NodeStore.applyBatch]) so taken reflects all earlier ops in
// the batch and no other writer can interleave.
func resolveGivenName(taken givenNames, selfName, base string) string {
	if base == "" {
		base = fallbackGivenName
	}

	candidate := base
	for i := 1; ; i++ {
		if taken.free(candidate, selfName) {
			return candidate
		}

		candidate = base + "-" + strconv.Itoa(i)
	}
}

// snapshotFromNodes builds the index maps and primary-route table for
// a new [Snapshot]. prevRoutes carries forward the previous primary
// assignment so a still-valid choice survives unrelated batches.
//
// keepPeers, when not nil, is the previous snapshot's peer relationship
// over the same node set; the caller vouches that the batch changed no
// input of it (see peerInputsChanged), so the policy scan is skipped and
// the views are re-pointed at the new copies.
func snapshotFromNodes(
	nodes map[types.NodeID]types.Node,
	peersFunc PeerPositionsFunc,
	prev PrimaryRouteLedger,
	keepPeers [][]int32,
) Snapshot {
	defer observeSince(nodeStoreSnapshotBuildDuration, time.Now())

	// One copy per node, shared by every index; each view points at it.
	allNodes := make([]types.NodeView, 0, len(nodes))
	nodeViewsByID := make(map[types.NodeID]types.NodeView, len(nodes))

	for _, n := range nodes {
		nodeView := n.View()
		allNodes = append(allNodes, nodeView)
		nodeViewsByID[n.ID] = nodeView
	}

	slices.SortFunc(allNodes, func(a, b types.NodeView) int { return cmp.Compare(a.ID(), b.ID()) })

	posByID := make(map[types.NodeID]int32, len(allNodes))
	for i, n := range allNodes {
		posByID[n.ID()] = int32(i)
	}

	routes, isPrimaryRoute := electPrimaryRoutes(nodes, prev.Global)
	regionalRoutes := electRegionalRoutes(nodes, prev.Regional)

	// The peer relationship is the expensive part: every pair of nodes
	// through the policy. It is only recomputed when a batch changed an
	// input of it.
	peerPositions := keepPeers
	if keepPeers == nil {
		start := time.Now()
		peerPositions = peersFunc(allNodes)

		observeSince(nodeStorePeersCalculationDuration, start)
	}

	newSnap := Snapshot{
		nodesByID:         nodes,
		nodeViewsByID:     nodeViewsByID,
		allNodes:          allNodes,
		nodesByNodeKey:    make(map[key.NodePublic]types.NodeView, len(nodes)),
		nodesByMachineKey: make(map[key.MachinePublic]machineKeyNodes, len(nodes)),
		posByID:           posByID,
		peerPositions:     peerPositions,
		nodesByUser:       make(map[types.UserID][]types.NodeView),

		routes:         routes,
		isPrimaryRoute: isPrimaryRoute,
		regionalRoutes: regionalRoutes,
	}

	// Build nodesByUser, nodesByNodeKey, and nodesByMachineKey maps
	for _, n := range nodes {
		nodeView := nodeViewsByID[n.ID]
		userID := n.TypedUserID()

		// Tagged nodes are owned by their tags, not a user,
		// so they are not indexed by user.
		if !n.IsTagged() {
			newSnap.nodesByUser[userID] = append(newSnap.nodesByUser[userID], nodeView)
		}

		newSnap.nodesByNodeKey[n.NodeKey] = nodeView

		// Build machine key index
		newSnap.nodesByMachineKey[n.MachineKey] = newSnap.nodesByMachineKey[n.MachineKey].add(userID, nodeView)
	}

	return newSnap
}

// onlineAdvertisers maps each non-exit prefix to the IDs of online nodes
// that approve it, in ascending node ID order.
//
// The order comes from sorting the per-prefix slices, which hold one or
// two IDs in practice, rather than from visiting the nodes in ID order:
// materialising and sorting every node ID costs a slice of the whole
// tailnet on every rebuild, including the common one where no node
// advertises a route at all. The map itself is only allocated once there
// is something to put in it.
func onlineAdvertisers(nodes map[types.NodeID]types.Node) map[netip.Prefix][]types.NodeID {
	var advertisers map[netip.Prefix][]types.NodeID

	for id, n := range nodes {
		if n.IsOnline == nil || !*n.IsOnline {
			continue
		}

		for _, p := range n.AllApprovedRoutes() {
			if tsaddr.IsExitRoute(p) {
				continue
			}

			if advertisers == nil {
				advertisers = make(map[netip.Prefix][]types.NodeID)
			}

			advertisers[p] = append(advertisers[p], id)
		}
	}

	for _, ids := range advertisers {
		slices.Sort(ids)
	}

	return advertisers
}

// electPrimaryRoutes picks the primary advertiser for each non-exit
// prefix. Inputs are restricted to online nodes that advertise the
// prefix. The previous primary is preserved when it is still online
// and healthy (anti-flap); otherwise the lowest-NodeID healthy
// advertiser wins. When every advertiser is unhealthy the previous
// primary is preserved only if still a candidate — falling back to
// any other candidate would point peers at a node the prober has
// already declared unreachable, so leaving the prefix unmapped is
// preferred until a probe cycle finds one that responds.
func electPrimaryRoutes(
	nodes map[types.NodeID]types.Node,
	prev map[netip.Prefix]types.NodeID,
) (map[netip.Prefix]types.NodeID, map[types.NodeID]bool) {
	advertisers := onlineAdvertisers(nodes)
	if len(advertisers) == 0 {
		// Nothing is advertised, so nothing is elected. Returning early
		// keeps a tailnet without subnet routers off the election path
		// entirely, which every write batch would otherwise pay for.
		return nil, nil
	}

	routes := electPrefixes(nodes, advertisers, prev)

	isPrimaryRoute := make(map[types.NodeID]bool, len(routes))
	for _, id := range routes {
		isPrimaryRoute[id] = true
	}

	return routes, isPrimaryRoute
}

// electPrefixes runs the primary election of [electPrimaryRoutes] over
// the given advertisers, carrying prev forward where it still holds.
func electPrefixes(
	nodes map[types.NodeID]types.Node,
	advertisers map[netip.Prefix][]types.NodeID,
	prev map[netip.Prefix]types.NodeID,
) map[netip.Prefix]types.NodeID {
	routes := make(map[netip.Prefix]types.NodeID, len(advertisers))
	for prefix, candidates := range advertisers {
		if cur, ok := prev[prefix]; ok &&
			slices.Contains(candidates, cur) &&
			!nodes[cur].Unhealthy {
			routes[prefix] = cur
			continue
		}

		var (
			selected types.NodeID
			found    bool
		)

		for _, c := range candidates {
			if !nodes[c].Unhealthy {
				selected = c
				found = true

				break
			}
		}

		// All-unhealthy fallback: preserve the previous primary only
		// when it is still a candidate. Falling back to any candidate
		// would point peers at a node the prober has already declared
		// unreachable; leaving the prefix unmapped is honest until a
		// probe cycle picks one that responds.
		if !found {
			if cur, ok := prev[prefix]; ok && slices.Contains(candidates, cur) {
				selected = cur
				found = true
			}
		}

		if found {
			routes[prefix] = selected
		}
	}

	return routes
}

// electRegionalRoutes runs the primary election once per DERP region over
// the region's own advertisers, so a viewer homed in a region with a
// working router for a prefix is steered to that router rather than the
// tailnet-wide primary (Tailscale's regional routing). Only prefixes with
// healthy advertisers in more than one region take part: with every
// advertiser in one region the tailnet-wide election already answers.
// Regions are read from each node's preferred DERP; nodes without one
// belong to no region.
func electRegionalRoutes(
	nodes map[types.NodeID]types.Node,
	prev map[tailcfg.DERPRegionID]map[netip.Prefix]types.NodeID,
) map[tailcfg.DERPRegionID]map[netip.Prefix]types.NodeID {
	advertisers := onlineAdvertisers(nodes)
	if len(advertisers) == 0 {
		return nil
	}

	byRegion := make(map[tailcfg.DERPRegionID]map[netip.Prefix][]types.NodeID)

	for prefix, candidates := range advertisers {
		regions := make(map[tailcfg.DERPRegionID][]types.NodeID)

		for _, id := range candidates {
			n := nodes[id]

			// An unhealthy router never holds a region: the region falls
			// back to the tailnet-wide primary, where the all-unhealthy
			// rule of [electPrefixes] applies.
			region := n.DERPRegion()
			if region == 0 || n.Unhealthy {
				continue
			}

			regions[region] = append(regions[region], id)
		}

		if len(regions) < 2 {
			continue
		}

		for region, ids := range regions {
			if byRegion[region] == nil {
				byRegion[region] = make(map[netip.Prefix][]types.NodeID)
			}

			byRegion[region][prefix] = ids
		}
	}

	if len(byRegion) == 0 {
		return nil
	}

	regional := make(map[tailcfg.DERPRegionID]map[netip.Prefix]types.NodeID, len(byRegion))
	for region, regionAdvertisers := range byRegion {
		regional[region] = electPrefixes(nodes, regionAdvertisers, prev[region])
	}

	return regional
}

// GetNode retrieves a node by its ID.
// The bool indicates if the node exists or is available (like "err not found").
// The [types.NodeView] might be invalid, so it must be checked with .Valid(), which must
// be used to ensure it isn't an invalid node (this is more of a node error or node is broken).
func (s *NodeStore) GetNode(id types.NodeID) (types.NodeView, bool) {
	defer nodeStoreGetMetrics.observe(time.Now())

	nodeView, exists := s.data.Load().nodeViewsByID[id]

	return nodeView, exists
}

// GetNodeByNodeKey retrieves a node by its [key.NodePublic].
// The bool indicates if the node exists or is available (like "err not found").
// The [types.NodeView] might be invalid, so it must be checked with .Valid(), which must
// be used to ensure it isn't an invalid node (this is more of a node error or node is broken).
func (s *NodeStore) GetNodeByNodeKey(nodeKey key.NodePublic) (types.NodeView, bool) {
	defer nodeStoreGetByKeyMetrics.observe(time.Now())

	nodeView, exists := s.data.Load().nodesByNodeKey[nodeKey]

	return nodeView, exists
}

// GetNodesByMachineKeyAllUsers returns every node sharing machineKey, keyed by
// owning UserID. Tagged nodes are indexed under UserID(0) (the tagged sentinel);
// user-owned nodes under their owning UserID. Returns an empty map if none.
//
// One machine key can map to several nodes (the same device registered by
// different users via the "create new, do not transfer" path). Exposing the
// whole set lets callers decide with full context — index [userID] for an exact
// match, [0] for a tagged node, or reject when the set is ambiguous — rather
// than guessing from a single arbitrary pick.
func (s *NodeStore) GetNodesByMachineKeyAllUsers(machineKey key.MachinePublic) map[types.UserID]types.NodeView {
	defer nodeStoreGetNodesByMachineKeyAllUsersMetrics.observe(time.Now())

	entry, ok := s.data.Load().nodesByMachineKey[machineKey]
	if !ok {
		return map[types.UserID]types.NodeView{}
	}

	out := make(map[types.UserID]types.NodeView, 1+len(entry.rest))
	out[entry.first.userID] = entry.first.node

	for _, e := range entry.rest {
		out[e.userID] = e.node
	}

	return out
}

// DebugString returns debug information about the [NodeStore].
func (s *NodeStore) DebugString() string {
	snapshot := s.data.Load()

	var sb strings.Builder

	sb.WriteString("=== NodeStore Debug Information ===\n\n")

	// Basic counts
	fmt.Fprintf(&sb, "Total Nodes: %d\n", len(snapshot.nodesByID))
	fmt.Fprintf(&sb, "Users with Nodes: %d\n", len(snapshot.nodesByUser))
	sb.WriteString("\n")

	// User distribution (shows internal UserID tracking, not display owner)
	sb.WriteString("Nodes by Internal User ID:\n")

	for userID, nodes := range snapshot.nodesByUser {
		if len(nodes) > 0 {
			userName := "unknown"

			if nodes[0].Valid() && nodes[0].User().Valid() {
				userName = nodes[0].User().Name()
			}

			fmt.Fprintf(&sb, "  - User %d (%s): %d nodes\n", userID, userName, len(nodes))
		}
	}

	sb.WriteString("\n")

	// Peer relationships summary
	sb.WriteString("Peer Relationships:\n")

	totalPeers := 0

	withEntry := 0

	for i, peers := range snapshot.peerPositions {
		if peers == nil {
			continue
		}

		withEntry++
		totalPeers += len(peers)

		node := snapshot.allNodes[i]
		fmt.Fprintf(&sb, "  - Node %d (%s): %d peers\n", node.ID(), node.Hostname(), len(peers))
	}

	if withEntry > 0 {
		avgPeers := float64(totalPeers) / float64(withEntry)
		fmt.Fprintf(&sb, "  - Average peers per node: %.1f\n", avgPeers)
	}

	sb.WriteString("\n")

	// Node key index
	fmt.Fprintf(&sb, "NodeKey Index: %d entries\n", len(snapshot.nodesByNodeKey))
	sb.WriteString("\n")

	return sb.String()
}

// ListNodes returns a slice of all nodes in the store.
func (s *NodeStore) ListNodes() views.Slice[types.NodeView] {
	defer nodeStoreListMetrics.observe(time.Now())

	return views.SliceOf(s.data.Load().allNodes)
}

// ListPeers returns a slice of all peers for a given node ID.
func (s *NodeStore) ListPeers(id types.NodeID) views.Slice[types.NodeView] {
	defer nodeStoreListPeersMetrics.observe(time.Now())

	return views.SliceOf(s.data.Load().peersOf(id))
}

// peersOf resolves a node's peers through allNodes, so they point at the
// snapshot's current copies whether or not the relationship was carried
// over from an earlier batch. Nil when the node has no entry.
func (snap *Snapshot) peersOf(id types.NodeID) []types.NodeView {
	pos, ok := snap.posByID[id]
	if !ok || int(pos) >= len(snap.peerPositions) {
		return nil
	}

	list := snap.peerPositions[pos]
	if list == nil {
		return nil
	}

	out := make([]types.NodeView, len(list))
	for i, p := range list {
		out[i] = snap.allNodes[p]
	}

	return out
}

// peersByNode materialises the whole relationship, for tests and the
// debug dump; readers use [Snapshot.peersOf].
func (snap *Snapshot) peersByNode() map[types.NodeID][]types.NodeView {
	if snap.peerPositions == nil {
		return nil
	}

	out := make(map[types.NodeID][]types.NodeView)

	for i, list := range snap.peerPositions {
		if list == nil {
			continue
		}

		out[snap.allNodes[i].ID()] = snap.peersOf(snap.allNodes[i].ID())
	}

	return out
}

// PrimaryRouteFor returns the current primary advertiser for prefix.
func (s *NodeStore) PrimaryRouteFor(prefix netip.Prefix) (types.NodeID, bool) {
	id, ok := s.data.Load().routes[prefix]
	return id, ok
}

// PrimaryRoutesForNode returns the prefixes for which id is the current
// primary advertiser.
func (s *NodeStore) PrimaryRoutesForNode(id types.NodeID) []netip.Prefix {
	snap := s.data.Load()
	if !snap.isPrimaryRoute[id] {
		return nil
	}

	out := make([]netip.Prefix, 0)

	for prefix, nodeID := range snap.routes {
		if nodeID == id {
			out = append(out, prefix)
		}
	}

	return out
}

// HANodes returns the prefixes with two or more online advertisers, the
// candidate set the HA prober needs to monitor.
func (s *NodeStore) HANodes() map[netip.Prefix][]types.NodeID {
	snap := s.data.Load()

	advertisers := onlineAdvertisers(snap.nodesByID)

	out := make(map[netip.Prefix][]types.NodeID)

	for p, ids := range advertisers {
		if len(ids) < 2 {
			continue
		}

		out[p] = ids
	}

	return out
}

// IsNodeHealthy reports whether the HA prober considers id healthy.
// Unknown nodes report healthy so absence does not exclude them from
// election.
func (s *NodeStore) IsNodeHealthy(id types.NodeID) bool {
	n, ok := s.data.Load().nodeViewsByID[id]
	if !ok {
		return true
	}

	return !n.Unhealthy()
}

// PrimaryRoutesForNodeAs returns the prefixes for which id is the primary
// advertiser as seen from a viewer homed in the given DERP region: the
// region's own primary where the region has one, the tailnet-wide primary
// elsewhere. Region 0 (unknown) sees the tailnet-wide assignment.
func (s *NodeStore) PrimaryRoutesForNodeAs(id types.NodeID, region tailcfg.DERPRegionID) []netip.Prefix {
	snap := s.data.Load()

	regional := snap.regionalRoutes[region]
	if len(regional) == 0 {
		return s.PrimaryRoutesForNode(id)
	}

	out := make([]netip.Prefix, 0)

	for prefix, nodeID := range snap.routes {
		if local, ok := regional[prefix]; ok {
			nodeID = local
		}

		if nodeID == id {
			out = append(out, prefix)
		}
	}

	return out
}

// RegionalRoutesDiffer reports whether a viewer moving from one DERP
// region to another would be steered to a different router for any
// prefix, so the caller knows when the move needs the viewer's peers
// rebuilt.
func (s *NodeStore) RegionalRoutesDiffer(from, to tailcfg.DERPRegionID) bool {
	snap := s.data.Load()
	if len(snap.regionalRoutes) == 0 || from == to {
		return false
	}

	for prefix, global := range snap.routes {
		before, ok := snap.regionalRoutes[from][prefix]
		if !ok {
			before = global
		}

		after, ok := snap.regionalRoutes[to][prefix]
		if !ok {
			after = global
		}

		if before != after {
			return true
		}
	}

	return false
}

// ledger returns the snapshot's primary assignment.
func (snap *Snapshot) ledger() PrimaryRouteLedger {
	return PrimaryRouteLedger{Global: snap.routes, Regional: snap.regionalRoutes}
}

// PrimaryRoutes returns the snapshot's primary assignment, tailnet-wide
// and per region. The maps are owned by the snapshot and must not be
// mutated; they are safe to read concurrently because snapshots are
// immutable once published.
func (s *NodeStore) PrimaryRoutes() PrimaryRouteLedger {
	return s.data.Load().ledger()
}

// PrimaryRoutesString renders the snapshot's prefix→primary map for
// debug output and test diagnostics.
func (s *NodeStore) PrimaryRoutesString() string {
	snap := s.data.Load()
	if len(snap.routes) == 0 {
		return ""
	}

	prefixes := make([]netip.Prefix, 0, len(snap.routes))
	for p := range snap.routes {
		prefixes = append(prefixes, p)
	}

	slices.SortFunc(prefixes, netip.Prefix.Compare)

	var b strings.Builder
	for _, p := range prefixes {
		fmt.Fprintf(&b, "%s: %d\n", p, snap.routes[p])
	}

	for _, region := range slices.Sorted(maps.Keys(snap.regionalRoutes)) {
		regional := snap.regionalRoutes[region]

		regionPrefixes := slices.Collect(maps.Keys(regional))
		slices.SortFunc(regionPrefixes, netip.Prefix.Compare)

		for _, p := range regionPrefixes {
			fmt.Fprintf(&b, "%s in DERP region %d: %d\n", p, region, regional[p])
		}
	}

	return b.String()
}

// RebuildPeerMaps rebuilds the peer relationship map using the current [PeersFunc].
// This must be called after policy changes because [PeersFunc] uses [policy.PolicyManager]'s
// filters to determine which nodes can see each other. Without rebuilding, the
// peer map would use stale filter data until the next node add/delete.
func (s *NodeStore) RebuildPeerMaps() {
	result := make(chan struct{})

	w := work{
		op:            rebuildPeerMaps,
		rebuildResult: result,
	}

	s.writeQueue <- w

	<-result
}

// ListNodesByUser returns a slice of all nodes for a given user ID.
func (s *NodeStore) ListNodesByUser(uid types.UserID) views.Slice[types.NodeView] {
	defer nodeStoreListByUserMetrics.observe(time.Now())

	return views.SliceOf(s.data.Load().nodesByUser[uid])
}

// updateNodes queues updates as one batch entry and waits for the batch
// to be applied. It reports whether the write ran; an empty update set or
// a stopped store drops it. Metrics belong to the caller so that
// [NodeStore.UpdateNode] is timed once instead of twice.
func (s *NodeStore) updateNodes(updates map[types.NodeID]UpdateNodeFunc) bool {
	if len(updates) == 0 {
		return false
	}

	w := work{
		op:           updateMulti,
		multiUpdates: updates,
		result:       make(chan struct{}),
	}

	nodeStoreQueueDepth.Inc()

	select {
	case s.writeQueue <- w:
	case <-s.stopped:
		nodeStoreQueueDepth.Dec()

		return false
	}

	<-w.result
	nodeStoreQueueDepth.Dec()

	return true
}

// applyBatch applies a batch of work to the node store.
// This means that it takes a copy of the current nodes,
// then applies the batch of operations to that copy,
// runs any precomputation needed (like calculating peers),
// and finally replaces the snapshot in the store with the new one.
// The replacement of the snapshot is atomic, ensuring that reads
// are never blocked by writes.
// Each write item is blocked until the batch is applied to ensure
// the caller knows the operation is complete and do not send any
// updates that are dependent on a read that is yet to be written.
//
// legacy: NodeStore write path; CLAUDE.md requires a benchmark before restructuring it,
// so it stays one function until measured.
//
//nolint:gocognit // NodeStore write path; CLAUDE.md requires benchmark before restructuring
func (s *NodeStore) applyBatch(batch []work) {
	defer observeSince(nodeStoreBatchDuration, time.Now())

	nodeStoreBatchSize.Observe(float64(len(batch)))

	prev := s.data.Load()

	nodes := make(map[types.NodeID]types.Node, len(prev.nodesByID))
	maps.Copy(nodes, prev.nodesByID)

	// names indexes the given names of every node. It is built on first
	// use so a batch that touches no name — the common map request
	// update — does not pay for it, and shared by every op in the batch
	// so the index is built once rather than per operation.
	var names givenNames

	ensureNames := func() givenNames {
		if names == nil {
			names = newGivenNames(nodes)
		}

		return names
	}

	// Track which work items need node results
	nodeResultRequests := make(map[types.NodeID][]*work)

	// Track rebuildPeerMaps operations
	var rebuildOps []*work

	// peersDirty is set once the batch touches an input of the peer
	// relationship; until then the previous one is carried forward.
	peersDirty := false

	// setErrResults collects per-work errors from the setName path so
	// they can be delivered after the snapshot swap, together with the
	// NodeView for that work.
	setErrResults := make(map[*work]error)

	for i := range batch {
		w := &batch[i]
		switch w.op {
		case put:
			peersDirty = true
			n := w.node
			taken := ensureNames()
			old, existed := nodes[n.ID]

			n.GivenName = resolveGivenName(taken, old.GivenName, n.GivenName)
			if existed {
				taken.release(old.GivenName)
			}

			taken.hold(n.GivenName)

			nodes[w.nodeID] = n
			if w.nodeResult != nil {
				nodeResultRequests[w.nodeID] = append(nodeResultRequests[w.nodeID], w)
			}
		case updateMulti:
			for id, fn := range w.multiUpdates {
				n, exists := nodes[id]
				if !exists {
					continue
				}

				old := n
				fn(&n)

				if !peersDirty && peerInputsChanged(&old, &n) {
					peersDirty = true
				}

				if oldGivenName := old.GivenName; n.GivenName != oldGivenName {
					taken := ensureNames()
					n.GivenName = resolveGivenName(taken, oldGivenName, n.GivenName)
					taken.release(oldGivenName)
					taken.hold(n.GivenName)
				}

				nodes[id] = n
			}
		case del:
			peersDirty = true

			if names != nil {
				if old, exists := nodes[w.nodeID]; exists {
					names.release(old.GivenName)
				}
			}

			delete(nodes, w.nodeID)
			// For delete operations, send an invalid NodeView if requested
			if w.nodeResult != nil {
				nodeResultRequests[w.nodeID] = append(nodeResultRequests[w.nodeID], w)
			}
		case setName:
			peersDirty = true

			n, exists := nodes[w.nodeID]
			if !exists {
				setErrResults[w] = ErrNodeNotFound
				nodeResultRequests[w.nodeID] = append(nodeResultRequests[w.nodeID], w)

				continue
			}

			if dnsname.ValidLabel(w.name) != nil {
				setErrResults[w] = ErrGivenNameInvalid
				nodeResultRequests[w.nodeID] = append(nodeResultRequests[w.nodeID], w)

				continue
			}

			taken := ensureNames()
			if !taken.free(w.name, n.GivenName) {
				setErrResults[w] = ErrGivenNameTaken
				nodeResultRequests[w.nodeID] = append(nodeResultRequests[w.nodeID], w)

				continue
			}

			taken.release(n.GivenName)
			taken.hold(w.name)

			n.GivenName = w.name
			nodes[w.nodeID] = n
			nodeResultRequests[w.nodeID] = append(nodeResultRequests[w.nodeID], w)
		case rebuildPeerMaps:
			// rebuildPeerMaps doesn't modify nodes, it just forces the snapshot rebuild
			// below to recalculate peer relationships using the current peersFunc
			rebuildOps = append(rebuildOps, w)
			peersDirty = true
		}
	}

	newSnap := s.nextSnapshot(prev, nodes, peersDirty)
	s.data.Store(&newSnap)

	// Update node count gauge
	nodeStoreNodesCount.Set(float64(len(nodes)))

	deliverBatchResults(batch, nodes, nodeResultRequests, setErrResults, rebuildOps)
}

// nextSnapshot builds the snapshot a batch produced, carrying the peer
// relationship forward when the batch changed none of its inputs.
func (s *NodeStore) nextSnapshot(prev *Snapshot, nodes map[types.NodeID]types.Node, peersDirty bool) Snapshot {
	var keepPeers [][]int32
	if !peersDirty {
		keepPeers = prev.peerPositions
	}

	return snapshotFromNodes(nodes, s.peersFunc, prev.ledger(), keepPeers)
}

// deliverBatchResults unblocks every writer waiting on the batch, after
// the new snapshot has been stored: first the work items that asked for
// the resulting node (and, for setName, its error), then the peer map
// rebuilds, then the rest.
func deliverBatchResults(
	batch []work,
	nodes map[types.NodeID]types.Node,
	nodeResultRequests map[types.NodeID][]*work,
	setErrResults map[*work]error,
	rebuildOps []*work,
) {
	// A zero-value NodeView{} reports Valid()==false, matching node.View()
	// for a node that was deleted or never existed.
	for nodeID, workItems := range nodeResultRequests {
		var nodeView types.NodeView
		if node, exists := nodes[nodeID]; exists {
			nodeView = node.View()
		}

		for _, w := range workItems {
			w.nodeResult <- nodeView

			close(w.nodeResult)

			if w.errResult != nil {
				w.errResult <- setErrResults[w]

				close(w.errResult)
			}
		}
	}

	for _, w := range rebuildOps {
		close(w.rebuildResult)
	}

	// Signal completion for all other work items
	for _, w := range batch {
		if w.op != rebuildPeerMaps {
			close(w.result)
		}
	}
}

// processWrite processes the write queue in batches.
func (s *NodeStore) processWrite() {
	c := time.NewTicker(s.batchTimeout)
	defer c.Stop()

	batch := make([]work, 0, s.batchSize)

	for {
		select {
		case w := <-s.writeQueue:
			batch = append(batch, w)
			if len(batch) >= s.batchSize {
				s.applyBatch(batch)
				batch = batch[:0]

				c.Reset(s.batchTimeout)
			}
		case <-c.C:
			if len(batch) != 0 {
				s.applyBatch(batch)
				batch = batch[:0]
			}

			c.Reset(s.batchTimeout)
		case <-s.stopped:
			// Apply any remaining batch so in-flight writers receive their
			// results, then exit.
			if len(batch) != 0 {
				s.applyBatch(batch)
			}

			return
		}
	}
}
