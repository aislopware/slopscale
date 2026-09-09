package state

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

// Tailscale Services on the server side; see docs/ref/services.md. A
// service is a name with addresses of its own; nodes report the services
// in their serve configuration over c2n, an operator or an auto-approver
// lets a node host one, and the elected host carries the service's
// addresses in the AllowedIPs its peers see.

// ErrVIPServiceUnknownName is returned when a node is approved for a
// service the tailnet does not have.
var ErrVIPServiceUnknownName = errors.New("unknown service")

// VIPServices returns the tailnet's services in name order.
func (s *State) VIPServices() []types.VIPService {
	services := s.vipServices.Load()
	if services == nil {
		return nil
	}

	return slices.Clone(*services)
}

// GetVIPService returns one service by name.
func (s *State) GetVIPService(name tailcfg.ServiceName) (types.VIPService, error) {
	for _, svc := range s.VIPServices() {
		if svc.Name == name {
			return svc, nil
		}
	}

	return types.VIPService{}, types.ErrVIPServiceNotFound
}

// loadVIPServices reads the services from the database and hands them
// to the policy manager. It runs at start and after every mutation.
func (s *State) loadVIPServices() (change.Change, error) {
	services, err := s.db.ListVIPServices()
	if err != nil {
		return change.Change{}, fmt.Errorf("loading services: %w", err)
	}

	s.vipServices.Store(&services)

	changed, err := s.polMan.SetVIPServices(services)
	if err != nil {
		return change.Change{}, fmt.Errorf("updating policy manager services: %w", err)
	}

	if !changed {
		return change.Change{}, nil
	}

	s.nodeStore.RebuildPeerMaps()

	return serviceChange(), nil
}

// serviceChange is what a service change needs: every node's peers may
// carry new addresses, every self node new caps and every DNS config a
// new record.
func serviceChange() change.Change {
	c := change.PolicyChange()
	c.Reason = "services"
	c.IncludeSelf = true

	return c
}

// CreateVIPService adds a service and gives it addresses.
func (s *State) CreateVIPService(
	name, displayName, comment string, ports []string,
) (types.VIPService, change.Change, error) {
	serviceName, err := types.ParseServiceName(name)
	if err != nil {
		return types.VIPService{}, change.Change{}, err
	}

	parsedPorts, err := types.ParseServicePorts(ports)
	if err != nil {
		return types.VIPService{}, change.Change{}, err
	}

	_, err = s.GetVIPService(serviceName)
	if err == nil {
		return types.VIPService{}, change.Change{}, types.ErrVIPServiceNameTaken
	}

	ipv4, ipv6, err := s.ipAlloc.Next()
	if err != nil {
		return types.VIPService{}, change.Change{}, fmt.Errorf("allocating service addresses: %w", err)
	}

	svc := types.VIPService{
		Name:        serviceName,
		DisplayName: strings.TrimSpace(displayName),
		Comment:     strings.TrimSpace(comment),
		Ports:       parsedPorts,
		IPv4:        ipv4,
		IPv6:        ipv6,
	}

	created, err := s.db.CreateVIPService(svc)
	if err != nil {
		s.ipAlloc.FreeIPs(svc.Addrs())

		return types.VIPService{}, change.Change{}, err
	}

	c, err := s.loadVIPServices()
	if err != nil {
		return types.VIPService{}, change.Change{}, err
	}

	log.Info().Str("service.name", string(created.Name)).Strs("service.addrs", addrStrings(created.Addrs())).
		Msg("service created")

	return created, c, nil
}

// UpdateVIPService replaces the display name, comment and ports of a
// service; nil leaves a field alone.
func (s *State) UpdateVIPService(
	name tailcfg.ServiceName, displayName, comment *string, ports *[]string,
) (types.VIPService, change.Change, error) {
	svc, err := s.GetVIPService(name)
	if err != nil {
		return types.VIPService{}, change.Change{}, err
	}

	if displayName != nil {
		svc.DisplayName = strings.TrimSpace(*displayName)
	}

	if comment != nil {
		svc.Comment = strings.TrimSpace(*comment)
	}

	if ports != nil {
		svc.Ports, err = types.ParseServicePorts(*ports)
		if err != nil {
			return types.VIPService{}, change.Change{}, err
		}
	}

	updated, err := s.db.UpdateVIPService(svc)
	if err != nil {
		return types.VIPService{}, change.Change{}, err
	}

	c, err := s.loadVIPServices()
	if err != nil {
		return types.VIPService{}, change.Change{}, err
	}

	return updated, c, nil
}

// DeleteVIPService removes a service, frees its addresses and takes it
// out of every node's approved list.
func (s *State) DeleteVIPService(name tailcfg.ServiceName) (change.Change, error) {
	svc, err := s.GetVIPService(name)
	if err != nil {
		return change.Change{}, err
	}

	err = s.db.DeleteVIPService(name)
	if err != nil {
		return change.Change{}, err
	}

	s.ipAlloc.FreeIPs(svc.Addrs())

	for _, node := range s.nodeStore.ListNodes().All() {
		if !node.ApprovedServices().ContainsFunc(func(n string) bool { return n == string(name) }) {
			continue
		}

		approved := slices.DeleteFunc(node.ApprovedServices().AsSlice(), func(n string) bool {
			return n == string(name)
		})

		err = s.storeApprovedServices(node.ID(), approved)
		if err != nil {
			return change.Change{}, err
		}
	}

	c, err := s.loadVIPServices()
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Str("service.name", string(name)).Msg("service deleted")

	return c.Merge(serviceChange()), nil
}

// SetApprovedServices replaces the services a node may host. Every name
// must exist and the node must be tagged: a host answers for a name that
// outlives any one person, as Tailscale requires.
func (s *State) SetApprovedServices(nodeID types.NodeID, names []string) (types.NodeView, change.Change, error) {
	node, ok := s.nodeStore.GetNode(nodeID)
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	approved := make([]string, 0, len(names))

	for _, raw := range names {
		name, err := types.ParseServiceName(raw)
		if err != nil {
			return types.NodeView{}, change.Change{}, err
		}

		_, err = s.GetVIPService(name)
		if err != nil {
			return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %s", ErrVIPServiceUnknownName, name)
		}

		if !slices.Contains(approved, string(name)) {
			approved = append(approved, string(name))
		}
	}

	if len(approved) > 0 && !node.IsTagged() {
		return types.NodeView{}, change.Change{}, types.ErrVIPServiceHostTagged
	}

	slices.Sort(approved)

	if slices.Equal(approved, node.ApprovedServices().AsSlice()) {
		return node, change.Change{}, nil
	}

	err := s.storeApprovedServices(nodeID, approved)
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	n, _ := s.nodeStore.GetNode(nodeID)

	_, err = s.updatePolicyManagerNodes()
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("updating policy manager after service approval: %w", err)
	}

	log.Info().EmbedObject(n).Strs("services.approved", approved).Msg("services approved")

	return n, serviceChange(), nil
}

// storeApprovedServices writes a node's approved list to the NodeStore
// and the database without recompiling.
func (s *State) storeApprovedServices(nodeID types.NodeID, approved []string) error {
	_, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		node.ApprovedServices = approved
	})
	if !ok {
		return fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	err := s.db.NodeSetApprovedServices(nodeID, approved)
	if err != nil {
		return fmt.Errorf("storing approved services: %w", err)
	}

	return nil
}

// AutoApproveServices approves the services the node reports that the
// policy's autoApprovers.services let it host. The returned change is
// empty when nothing was added.
func (s *State) AutoApproveServices(nv types.NodeView) (change.Change, error) {
	if !nv.Valid() || !nv.IsTagged() {
		return change.Change{}, nil
	}

	approved := nv.ApprovedServices().AsSlice()

	for _, svc := range nv.AnnouncedServices() {
		if slices.Contains(approved, string(svc.Name)) {
			continue
		}

		_, err := s.GetVIPService(svc.Name)
		if err != nil {
			continue
		}

		if s.polMan.NodeCanApproveService(nv, svc.Name) {
			approved = append(approved, string(svc.Name))
		}
	}

	if len(approved) == nv.ApprovedServices().Len() {
		return change.Change{}, nil
	}

	_, c, err := s.SetApprovedServices(nv.ID(), approved)

	return c, err
}

// ServicesReportStale reports whether the node's Hostinfo names a newer
// service list than the one the server holds, so the server should ask
// for it over c2n.
func (s *State) ServicesReportStale(nodeID types.NodeID) bool {
	node, ok := s.nodeStore.GetNode(nodeID)
	if !ok || !node.Hostinfo().Valid() {
		return false
	}

	return node.Hostinfo().ServicesHash() != node.ServicesHash()
}

// CollectVIPServices asks a connected node for the services in its serve
// configuration and records the answer, approving what the policy lets
// it host. The returned change is non-empty when the hosts of a service
// changed.
func (s *State) CollectVIPServices(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) (change.Change, error) {
	if !connected {
		return change.Change{}, ErrNodeNotConnected
	}

	resp, err := s.c2nRoundTrip(ctx, nodeID, http.MethodGet, "/vip-services", dispatch)
	if err != nil {
		return change.Change{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return change.Change{}, fmt.Errorf("%w: %s", ErrC2NFailed, resp.Status)
	}

	var report tailcfg.C2NVIPServicesResponse

	err = readJSON(resp.Body, &report)
	if err != nil {
		return change.Change{}, fmt.Errorf("decoding services: %w", err)
	}

	services := &types.NodeServices{Hash: report.ServicesHash}

	for _, svc := range report.VIPServices {
		if svc == nil {
			continue
		}

		services.Services = append(services.Services, *svc)
	}

	slices.SortFunc(services.Services, func(a, b tailcfg.VIPService) int {
		return strings.Compare(string(a.Name), string(b.Name))
	})

	return s.setNodeServices(nodeID, services)
}

// setNodeServices stores a report and recomputes the policy when the
// hosted services changed.
func (s *State) setNodeServices(nodeID types.NodeID, services *types.NodeServices) (change.Change, error) {
	var before types.Node

	n, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		before = *node
		node.Services = services
	})
	if !ok {
		return change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	err := s.db.NodeSetServices(nodeID, services)
	if err != nil {
		return change.Change{}, fmt.Errorf("storing services: %w", err)
	}

	log.Debug().Uint64(zf.NodeID, nodeID.Uint64()).Int("services.count", len(services.Services)).
		Msg("services reported")

	c, err := s.updatePolicyManagerNodes()
	if err != nil {
		return change.Change{}, err
	}

	approval, err := s.AutoApproveServices(n)
	if err != nil {
		return change.Change{}, err
	}

	c = c.Merge(approval)

	if c.IsEmpty() && !before.View().ServicesEqualForPeers(n) {
		c = serviceChange()
	}

	return c, nil
}

// ServiceHosts elects one host per service: among the nodes approved
// for it that advertise it, an online one before an offline one, then
// the lowest ID, so every peer routes the service's addresses to the
// same node until it goes away.
func (s *State) ServiceHosts() map[tailcfg.ServiceName]types.NodeID {
	services := s.VIPServices()
	if len(services) == 0 {
		return nil
	}

	known := make(map[tailcfg.ServiceName]struct{}, len(services))
	for _, svc := range services {
		known[svc.Name] = struct{}{}
	}

	type candidate struct {
		id     types.NodeID
		online bool
	}

	best := make(map[tailcfg.ServiceName]candidate)

	for _, node := range s.nodeStore.ListNodes().All() {
		if !node.IsAdmitted() || node.IsExpired() {
			continue
		}

		online := node.IsOnline().Valid() && node.IsOnline().Get()

		for _, name := range node.HostedServices() {
			if _, ok := known[name]; !ok {
				continue
			}

			if _, active := node.HostsService(name); !active {
				continue
			}

			current, ok := best[name]
			if !ok || (online && !current.online) || (online == current.online && node.ID() < current.id) {
				best[name] = candidate{id: node.ID(), online: online}
			}
		}
	}

	out := make(map[tailcfg.ServiceName]types.NodeID, len(best))
	for name, c := range best {
		out[name] = c.id
	}

	return out
}

// ServiceRoutesFor returns the addresses of the services the node is the
// elected host of, as single-address prefixes for its AllowedIPs.
func (s *State) ServiceRoutesFor(nodeID types.NodeID, hosts map[tailcfg.ServiceName]types.NodeID) []netip.Prefix {
	if len(hosts) == 0 {
		return nil
	}

	var names []tailcfg.ServiceName

	for name, id := range hosts {
		if id == nodeID {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return nil
	}

	slices.Sort(names)

	var out []netip.Prefix

	for _, svc := range s.VIPServices() {
		if slices.Contains(names, svc.Name) {
			out = append(out, svc.Prefixes()...)
		}
	}

	return out
}

// ServiceDNSRecords are the MagicDNS records of the services that have a
// host: <name>.<base domain> to the service's addresses.
func (s *State) ServiceDNSRecords(baseDomain string) []tailcfg.DNSRecord {
	if baseDomain == "" {
		return nil
	}

	hosts := s.ServiceHosts()
	if len(hosts) == 0 {
		return nil
	}

	var out []tailcfg.DNSRecord

	for _, svc := range s.VIPServices() {
		if _, ok := hosts[svc.Name]; !ok {
			continue
		}

		for _, addr := range svc.Addrs() {
			out = append(out, tailcfg.DNSRecord{Name: svc.DNSName(baseDomain), Value: addr.String()})
		}
	}

	return out
}

func addrStrings(addrs []netip.Addr) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}

	return out
}
