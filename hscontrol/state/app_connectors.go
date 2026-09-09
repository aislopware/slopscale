package state

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
	"tailscale.com/types/views"
)

// Apps are sets of domains reached through app connectors; the table is
// handed to the policy manager, which stamps the connector nodes and
// lets them approve the routes they learn. See docs/ref/apps.md.

// AppConnectors returns the tailnet's apps in name order.
func (s *State) AppConnectors() []types.AppConnector {
	apps := s.appConnectors.Load()
	if apps == nil {
		return nil
	}

	return slices.Clone(*apps)
}

// GetAppConnector returns one app.
func (s *State) GetAppConnector(id types.AppConnectorID) (types.AppConnector, error) {
	for _, app := range s.AppConnectors() {
		if app.ID == id {
			return app, nil
		}
	}

	return types.AppConnector{}, types.ErrAppConnectorNotFound
}

// loadAppConnectors reads the apps from the database and hands them to
// the policy manager. It runs at start and after every mutation.
func (s *State) loadAppConnectors() (change.Change, error) {
	apps, err := s.db.ListAppConnectors()
	if err != nil {
		return change.Change{}, fmt.Errorf("loading apps: %w", err)
	}

	s.appConnectors.Store(&apps)

	changed, err := s.polMan.SetAppConnectors(apps)
	if err != nil {
		return change.Change{}, fmt.Errorf("updating policy manager apps: %w", err)
	}

	if !changed {
		return change.Change{}, nil
	}

	s.nodeStore.RebuildPeerMaps()

	return appConnectorChange(), nil
}

// appConnectorChange is what an app change needs: the connector nodes'
// self caps change, and once a connector approves routes its peers see
// new allowed IPs.
func appConnectorChange() change.Change {
	c := change.PolicyChange()
	c.Reason = "apps"
	c.IncludeSelf = true

	return c
}

// applyAppConnectors reloads the apps and approves whatever routes the
// connectors already advertise that the apps now cover.
func (s *State) applyAppConnectors() (change.Change, error) {
	c, err := s.loadAppConnectors()
	if err != nil {
		return change.Change{}, err
	}

	approvals, err := s.autoApproveNodes()
	if err != nil {
		return change.Change{}, err
	}

	for _, ac := range approvals {
		c = c.Merge(ac)
	}

	return c, nil
}

// CreateAppConnector adds an app.
func (s *State) CreateAppConnector(app types.AppConnector) (types.AppConnector, change.Change, error) {
	err := app.Normalize()
	if err != nil {
		return types.AppConnector{}, change.Change{}, err
	}

	created, err := s.db.CreateAppConnector(app)
	if err != nil {
		return types.AppConnector{}, change.Change{}, err
	}

	c, err := s.applyAppConnectors()
	if err != nil {
		return types.AppConnector{}, change.Change{}, err
	}

	log.Info().Str("app.name", created.Name).Strs("app.domains", created.Domains).Msg("app created")

	return created, c, nil
}

// UpdateAppConnector replaces an app's definition.
func (s *State) UpdateAppConnector(app types.AppConnector) (types.AppConnector, change.Change, error) {
	existing, err := s.GetAppConnector(app.ID)
	if err != nil {
		return types.AppConnector{}, change.Change{}, err
	}

	err = app.Normalize()
	if err != nil {
		return types.AppConnector{}, change.Change{}, err
	}

	app.CreatedAt = existing.CreatedAt

	updated, err := s.db.UpdateAppConnector(app)
	if err != nil {
		return types.AppConnector{}, change.Change{}, err
	}

	c, err := s.applyAppConnectors()
	if err != nil {
		return types.AppConnector{}, change.Change{}, err
	}

	return updated, c, nil
}

// DeleteAppConnector removes an app. Routes its connectors already had
// approved stay approved; an operator withdraws them on the node.
func (s *State) DeleteAppConnector(id types.AppConnectorID) (change.Change, error) {
	app, err := s.GetAppConnector(id)
	if err != nil {
		return change.Change{}, err
	}

	err = s.db.DeleteAppConnector(id)
	if err != nil {
		return change.Change{}, err
	}

	c, err := s.loadAppConnectors()
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Str("app.name", app.Name).Msg("app deleted")

	return c.Merge(appConnectorChange()), nil
}

// AppConnectorNodes returns the nodes the app's connector selectors pick,
// in ID order.
func (s *State) AppConnectorNodes(app types.AppConnector) []types.NodeView {
	var out []types.NodeView

	for _, node := range s.nodeStore.ListNodes().All() {
		tags := node.Tags().AsSlice()
		runsConnector := node.Hostinfo().Valid() && node.Hostinfo().AppConnector().EqualBool(true)

		if app.Selects(tags, runsConnector) {
			out = append(out, node)
		}
	}

	return out
}

// AppDNSRoutes returns the split DNS the node needs for the apps: each
// app domain resolved through a connector's PeerAPI, which is how
// Tailscale's clients send the app's queries to the connector so it can
// learn the addresses (dnstype.Resolver's http:// form). Only connectors
// the node sees as peers are offered, online ones first; a connector
// gets no routes for its own apps. A wildcard domain covers its base.
func (s *State) AppDNSRoutes(node types.NodeView) map[string][]*dnstype.Resolver {
	apps := s.AppConnectors()
	if len(apps) == 0 {
		return nil
	}

	peers := s.ListPeers(node.ID())
	tags := node.Tags().AsSlice()
	runsConnector := node.Hostinfo().Valid() && node.Hostinfo().AppConnector().EqualBool(true)

	var routes map[string][]*dnstype.Resolver

	for _, app := range apps {
		if app.Selects(tags, runsConnector) {
			continue
		}

		resolvers := connectorResolvers(app, node, peers)
		if len(resolvers) == 0 {
			continue
		}

		for _, domain := range app.Domains {
			domain = strings.ToLower(strings.TrimPrefix(domain, "*."))

			if routes == nil {
				routes = make(map[string][]*dnstype.Resolver)
			}

			routes[domain] = append(routes[domain], resolvers...)
		}
	}

	return routes
}

// connectorResolvers lists the app's connectors among the peers as
// PeerAPI DNS resolvers, chosen for the address family the node has.
func connectorResolvers(
	app types.AppConnector, node types.NodeView, peers views.Slice[types.NodeView],
) []*dnstype.Resolver {
	var online, offline []*dnstype.Resolver

	for _, peer := range peers.All() {
		if !peer.Hostinfo().Valid() || !peer.Hostinfo().AppConnector().EqualBool(true) {
			continue
		}

		if !app.Selects(peer.Tags().AsSlice(), true) {
			continue
		}

		addr := peerAPIDNS(node, peer)
		if addr == "" {
			continue
		}

		r := &dnstype.Resolver{Addr: addr}
		if peer.IsOnline().Valid() && peer.IsOnline().Get() {
			online = append(online, r)
		} else {
			offline = append(offline, r)
		}
	}

	return append(online, offline...)
}

// peerAPIDNS is the peer's PeerAPI DNS endpoint reachable from the node:
// its IPv4 one when both have IPv4, else IPv6; empty when the peer
// reports no PeerAPI.
func peerAPIDNS(node, peer types.NodeView) string {
	var have4, have6 bool

	for _, ip := range node.IPs() {
		if ip.Is4() {
			have4 = true
		} else {
			have6 = true
		}
	}

	var port4, port6 uint16

	for _, svc := range peer.Hostinfo().Services().All() {
		switch svc.Proto {
		case tailcfg.PeerAPI4:
			port4 = svc.Port
		case tailcfg.PeerAPI6:
			port6 = svc.Port
		case tailcfg.TCP, tailcfg.UDP, tailcfg.PeerAPIDNS:
		default:
		}
	}

	for _, ip := range peer.IPs() {
		if have4 && port4 != 0 && ip.Is4() {
			return "http://" + netip.AddrPortFrom(ip, port4).String() + "/dns-query"
		}

		if have6 && port6 != 0 && ip.Is6() {
			return "http://" + netip.AddrPortFrom(ip, port6).String() + "/dns-query"
		}
	}

	return ""
}
