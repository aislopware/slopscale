package dockertestutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	mobynetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

var (
	ErrContainerNotFound = errors.New("container not found")
	ErrNetworkNotFound   = errors.New("network not found")
	ErrConditionTimeout  = errors.New("condition not met within timeout")
)

// Network is a docker network the harness created. It satisfies
// [dockertest.Network], so containers connect to it through dockertest,
// while creation goes through the moby client for the IPAM subnet.
type Network struct {
	docker  *client.Client
	inspect mobynetwork.Inspect
}

// ID returns the network's docker ID.
func (n *Network) ID() string {
	return n.inspect.ID
}

// Name returns the network's name.
func (n *Network) Name() string {
	return n.inspect.Name
}

// Inspect returns the network as the daemon reported it at creation.
func (n *Network) Inspect() mobynetwork.Inspect {
	return n.inspect
}

// Close removes the network. Containers must have left it.
func (n *Network) Close() error {
	_, err := n.docker.NetworkRemove(context.Background(), n.inspect.ID, client.NetworkRemoveOptions{})
	if err != nil {
		return fmt.Errorf("removing network %s: %w", n.inspect.Name, err)
	}

	return nil
}

// retryDockerOp absorbs eventual-consistency races in libnetwork endpoint cleanup.
// Pulls its backoff bounds from retry.go so every helper that drives a
// docker control-plane call uses the same budget.
func retryDockerOp(ctx context.Context, op func() error) error {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = DockerOpInitialInterval
	bo.MaxInterval = DockerOpMaxInterval

	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		return struct{}{}, op()
	}, backoff.WithBackOff(bo), backoff.WithMaxElapsedTime(DockerOpMaxElapsedTime))

	return err
}

func GetFirstOrCreateNetwork(pool *Pool, name string) (*Network, error) {
	return GetFirstOrCreateNetworkWithSubnet(pool, name, "")
}

// GetFirstOrCreateNetworkWithSubnet creates a Docker network with an optional
// custom subnet. When subnet is empty, Docker auto-assigns from its default
// pool. Use RFC 5737 TEST-NET ranges (e.g. "198.51.100.0/24") for networks
// that need to be reachable through Tailscale exit nodes, since Tailscale's
// shrinkDefaultRoute strips RFC1918 ranges from exit node forwarding filters.
func GetFirstOrCreateNetworkWithSubnet(pool *Pool, name, subnet string) (*Network, error) {
	ctx := context.Background()

	network, err := networkByName(ctx, pool, name)
	if err == nil {
		return network, nil
	}

	if !errors.Is(err, ErrNetworkNotFound) {
		return nil, err
	}

	opts := client.NetworkCreateOptions{}

	if subnet != "" {
		prefix, parseErr := netip.ParsePrefix(subnet)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing subnet %q: %w", subnet, parseErr)
		}

		opts.IPAM = &mobynetwork.IPAM{
			Config: []mobynetwork.IPAMConfig{{Subnet: prefix}},
		}
	}

	_, err = pool.Docker.NetworkCreate(ctx, name, opts)
	if err != nil {
		return nil, fmt.Errorf("creating network: %w", err)
	}

	// Create does not give us an updated version of the resource, so we need to
	// get it again.
	return networkByName(ctx, pool, name)
}

// networkByName resolves an exact network name; docker's name filter
// matches substrings, so the result is checked.
func networkByName(ctx context.Context, pool *Pool, name string) (*Network, error) {
	list, err := pool.Docker.NetworkList(ctx, client.NetworkListOptions{
		Filters: make(client.Filters).Add("name", name),
	})
	if err != nil {
		return nil, fmt.Errorf("looking up network names: %w", err)
	}

	for _, summary := range list.Items {
		if summary.Name != name {
			continue
		}

		info, inspectErr := pool.Docker.NetworkInspect(ctx, summary.ID, client.NetworkInspectOptions{})
		if inspectErr != nil {
			return nil, fmt.Errorf("inspecting network %s: %w", name, inspectErr)
		}

		return &Network{docker: pool.Docker, inspect: info.Network}, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrNetworkNotFound, name)
}

func AddContainerToNetwork(
	pool *Pool,
	network *Network,
	testContainer string,
) error {
	containerID, err := lookupContainerID(pool, testContainer)
	if err != nil {
		return err
	}

	return retryDockerOp(context.Background(), func() error {
		return connectNetwork(pool, network, containerID)
	})
}

func connectNetwork(pool *Pool, network *Network, containerID string) error {
	_, err := pool.Docker.NetworkConnect(context.Background(), network.ID(), client.NetworkConnectOptions{
		Container: containerID,
	})

	return err
}

// DisconnectContainerFromNetwork detaches the container at the docker
// daemon level (cable-pull semantics) and waits for libnetwork to drop
// the endpoint before returning — re-attaching during the
// reprogramming window otherwise fails with "network is unreachable".
func DisconnectContainerFromNetwork(
	pool *Pool,
	network *Network,
	testContainer string,
) error {
	containerID, err := lookupContainerID(pool, testContainer)
	if err != nil {
		return err
	}

	err = retryDockerOp(context.Background(), func() error {
		_, disconnectErr := pool.Docker.NetworkDisconnect(
			context.Background(),
			network.ID(),
			client.NetworkDisconnectOptions{Container: containerID},
		)

		return disconnectErr
	})
	if err != nil {
		return err
	}

	err = waitNetworkContainerAbsent(pool, network, testContainer, DockerOpMaxElapsedTime)
	if err != nil {
		return err
	}

	// libnetwork drops the endpoint from its model before the kernel
	// netns has flushed the matching route. Re-attach with the sticky
	// IP otherwise fails with "conflicts with existing route".
	return waitContainerRouteAbsent(pool, containerID, network, DockerOpMaxElapsedTime)
}

// ReconnectContainerToNetwork is the inverse of
// [DisconnectContainerFromNetwork] — re-attaches the container to the
// network so traffic can flow again.
func ReconnectContainerToNetwork(
	pool *Pool,
	network *Network,
	testContainer string,
) error {
	containerID, err := lookupContainerID(pool, testContainer)
	if err != nil {
		return err
	}

	err = retryDockerOp(context.Background(), func() error {
		connectErr := connectNetwork(pool, network, containerID)
		if connectErr != nil && isStaleRouteConflict(connectErr) {
			// Defensive cleanup: a route survived the netns flush
			// despite the wait above. Drop subnet routes that point
			// at the disconnected interface so libnetwork can
			// reprogram the sticky IP, then let the retry budget
			// try the connect call again.
			removeContainerSubnetRoutes(pool, containerID, network)
		}

		return connectErr
	})
	if err != nil {
		return err
	}

	return waitNetworkContainerPresent(pool, network, testContainer, DockerOpMaxElapsedTime)
}

// lookupContainerID resolves an exact container name to its docker ID;
// docker's name filter matches substrings, so the names are checked.
func lookupContainerID(pool *Pool, testContainer string) (string, error) {
	list, err := pool.Docker.ContainerList(context.Background(), client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("name", testContainer),
	})
	if err != nil {
		return "", err
	}

	for _, c := range list.Items {
		for _, name := range c.Names {
			if strings.TrimPrefix(name, "/") == testContainer {
				return c.ID, nil
			}
		}
	}

	return "", fmt.Errorf("%w: %s", ErrContainerNotFound, testContainer)
}

// DisconnectAndReconnect calls Disconnect followed by Reconnect; both
// primitives drive their own libnetwork settle waits.
func DisconnectAndReconnect(
	pool *Pool,
	network *Network,
	testContainer string,
) error {
	err := DisconnectContainerFromNetwork(pool, network, testContainer)
	if err != nil {
		return fmt.Errorf("disconnecting %s from %s: %w", testContainer, network.Name(), err)
	}

	err = ReconnectContainerToNetwork(pool, network, testContainer)
	if err != nil {
		return fmt.Errorf("reconnecting %s to %s: %w", testContainer, network.Name(), err)
	}

	return nil
}

func waitNetworkContainer(
	pool *Pool,
	network *Network,
	testContainer string,
	timeout time.Duration,
	want bool,
	match func(mobynetwork.EndpointResource) bool,
) error {
	return pollUntil(timeout, func() (bool, error) {
		info, err := pool.Docker.NetworkInspect(context.Background(), network.ID(), client.NetworkInspectOptions{})
		if err != nil {
			return false, fmt.Errorf("inspecting network %s: %w", network.Name(), err)
		}

		found := false

		for _, c := range info.Network.Containers {
			if (c.Name == testContainer || c.Name == "/"+testContainer) && match(c) {
				found = true
				break
			}
		}

		return found == want, nil
	})
}

func waitNetworkContainerAbsent(
	pool *Pool,
	network *Network,
	testContainer string,
	timeout time.Duration,
) error {
	return waitNetworkContainer(
		pool,
		network,
		testContainer,
		timeout,
		false,
		func(mobynetwork.EndpointResource) bool { return true },
	)
}

func waitNetworkContainerPresent(
	pool *Pool,
	network *Network,
	testContainer string,
	timeout time.Duration,
) error {
	return waitNetworkContainer(
		pool,
		network,
		testContainer,
		timeout,
		true,
		func(c mobynetwork.EndpointResource) bool { return c.IPv4Address.IsValid() },
	)
}

// waitContainerRouteAbsent polls the container's routing table until no
// route remains for the network's IPAM subnet. libnetwork's docker-side
// endpoint teardown is asynchronous from the kernel netns flush, and a
// surviving route blocks a subsequent reconnect at sticky-IP assignment
// with "conflicts with existing route".
func waitContainerRouteAbsent(
	pool *Pool,
	containerID string,
	network *Network,
	timeout time.Duration,
) error {
	subnets := networkSubnets(network)
	if len(subnets) == 0 {
		return nil
	}

	return pollUntil(timeout, func() (bool, error) {
		stdout, err := execStdout(pool, containerID, []string{"ip", "-4", "route", "show"})
		if err != nil {
			return false, fmt.Errorf("inspecting routes in %s: %w", containerID, err)
		}

		for _, subnet := range subnets {
			if strings.Contains(stdout, subnet+" ") || strings.HasSuffix(strings.TrimSpace(stdout), subnet) {
				return false, nil
			}
		}

		return true, nil
	})
}

// removeContainerSubnetRoutes drops residue subnet routes in the
// container's netns — the leftover that libnetwork's async endpoint
// teardown can leave behind.
func removeContainerSubnetRoutes(pool *Pool, containerID string, network *Network) {
	for _, subnet := range networkSubnets(network) {
		_, err := execStdout(pool, containerID, []string{"ip", "-4", "route", "del", subnet})
		if err != nil {
			log.Printf("removing stale route %s in %s: %v", subnet, containerID, err)
		}
	}
}

// isStaleRouteConflict matches the libnetwork 500 raised when a
// surviving subnet route blocks sticky-IP reprogramming on reconnect.
func isStaleRouteConflict(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "conflicts with existing route")
}

// networkSubnets returns the IPAM-configured subnets for a docker
// network. Empty when IPAM is left to docker defaults.
func networkSubnets(network *Network) []string {
	config := network.Inspect().IPAM.Config

	out := make([]string, 0, len(config))

	for _, cfg := range config {
		if cfg.Subnet.IsValid() {
			out = append(out, cfg.Subnet.String())
		}
	}

	return out
}

// execStdout runs a one-shot command in containerID and returns stdout.
func execStdout(pool *Pool, containerID string, cmd []string) (string, error) {
	var stdout, stderr bytes.Buffer

	_, err := execInContainer(context.Background(), pool.Docker, containerID, cmd, nil, &stdout, &stderr)
	if err != nil {
		return stdout.String(), err
	}

	return stdout.String(), nil
}

// execInContainer runs cmd in the container with env, copies its output
// into stdout and stderr and returns the exit code.
func execInContainer(
	ctx context.Context,
	docker *client.Client,
	containerID string,
	cmd, env []string,
	stdout, stderr *bytes.Buffer,
) (int, error) {
	created, err := docker.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd:          cmd,
		Env:          env,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return 0, fmt.Errorf("create exec: %w", err)
	}

	attached, err := docker.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return 0, fmt.Errorf("start exec: %w", err)
	}
	defer attached.Close()

	// The hijacked connection outlives ctx, so a command that never
	// exits (tailscale up waiting for a login) has to be cut off here,
	// keeping what it printed so far the way v3 did.
	copied := make(chan error, 1)

	go func() {
		_, copyErr := stdcopy.StdCopy(stdout, stderr, attached.Reader)
		copied <- copyErr
	}()

	select {
	case err = <-copied:
		if err != nil {
			return 0, fmt.Errorf("read exec: %w", err)
		}
	case <-ctx.Done():
		attached.Close()
		<-copied

		return 0, ctx.Err()
	}

	info, err := docker.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return 0, fmt.Errorf("inspect exec: %w", err)
	}

	return info.ExitCode, nil
}

// pollUntil ticks every DockerOpInitialInterval until check returns
// done=true or timeout elapses. A non-nil check error aborts the loop.
func pollUntil(timeout time.Duration, check func() (done bool, err error)) error {
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(DockerOpInitialInterval)
	defer ticker.Stop()

	for {
		done, err := check()
		if err != nil {
			return err
		}

		if done {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s", ErrConditionTimeout, timeout)
		}

		<-ticker.C
	}
}

// RandomFreeHostPort asks the kernel for a free open port that is ready to use.
// (from https://github.com/phayes/freeport)
func RandomFreeHostPort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", listener.Addr())
	}

	return tcpAddr.Port, nil
}

// DockerRestartPolicy sets the restart policy for containers.
func DockerRestartPolicy(config *container.HostConfig) {
	config.RestartPolicy = container.RestartPolicy{
		Name: container.RestartPolicyUnlessStopped,
	}
}

// DockerAllowLocalIPv6 allows IPv6 traffic within the container.
func DockerAllowLocalIPv6(config *container.HostConfig) {
	config.Sysctls = map[string]string{
		"net.ipv6.conf.all.disable_ipv6": "0",
	}
}

// DockerAllowNetworkAdministration gives the container network administration capabilities.
func DockerAllowNetworkAdministration(config *container.HostConfig) {
	config.CapAdd = append(config.CapAdd, "NET_ADMIN")
	config.Privileged = true
}

// DockerMemoryLimit sets memory limit and disables OOM kill for containers.
func DockerMemoryLimit(config *container.HostConfig) {
	config.Memory = 2 * 1024 * 1024 * 1024 // 2GB in bytes
	config.OomKillDisable = new(true)
}
