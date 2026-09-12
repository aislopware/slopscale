package integration

import (
	"io"
	"net/netip"
	"net/url"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/aislopware/slopscale/integration/dockertestutil"
	"github.com/aislopware/slopscale/integration/tsic"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/netcheck"
	"tailscale.com/types/key"
	"tailscale.com/types/netmap"
	"tailscale.com/wgengine/filter"
)

//nolint:interfacebloat // the test client mirrors the whole tailscale CLI surface
type TailscaleClient interface {
	Hostname() string
	Shutdown() (string, string, error)
	Version() string
	Execute(
		command []string,
		options ...dockertestutil.ExecuteCommandOption,
	) (string, string, error)
	Login(loginServer, authKey string) error
	LoginWithURL(loginServer string) (*url.URL, error)
	Logout() error
	Restart() error
	Up() error
	Down() error
	IPs() ([]netip.Addr, error)
	MustIPs() []netip.Addr
	IPv4() (netip.Addr, error)
	MustIPv4() netip.Addr
	MustIPv6() netip.Addr
	FQDN() (string, error)
	MustFQDN() string
	Status(_ ...bool) (*ipnstate.Status, error)
	MustStatus() *ipnstate.Status
	Netmap() (*netmap.NetworkMap, error)
	DebugDERPRegion(region string) (*ipnstate.DebugDERPRegionReport, error)
	GetNodePrivateKey() (*key.NodePrivate, error)
	Netcheck() (*netcheck.Report, error)
	WaitForNeedsLogin(timeout time.Duration) error
	WaitForRunning(timeout time.Duration) error
	WaitForPeers(expected int, timeout, retryInterval time.Duration) error
	Ping(hostnameOrIP string, opts ...tsic.PingOption) error
	Curl(url string, opts ...tsic.CurlOption) (string, error)
	CurlFailFast(url string) (string, error)
	Traceroute(ip netip.Addr) (util.Traceroute, error)
	ContainerID() string
	MustID() types.NodeID
	ReadFile(path string) ([]byte, error)
	PacketFilter() ([]filter.Match, error)
	ConnectToNetwork(network *dockertestutil.Network) error
	DisconnectFromNetwork(network *dockertestutil.Network) error
	ReconnectToNetwork(network *dockertestutil.Network) error

	// FailingPeersAsString returns a formatted-ish multi-line-string of peers in the client
	// and a bool indicating if the clients online count and peer count is equal.
	FailingPeersAsString() (string, bool, error)

	WriteLogs(stdout, stderr io.Writer) error
}
