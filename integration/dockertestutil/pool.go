package dockertestutil

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/ory/dockertest/v4"
)

// Pool is dockertest's pool plus the moby client it was built on, for
// what the pool does not expose: exec with an environment, file copy,
// logs, restart, network IPAM and lookups by name.
type Pool struct {
	dockertest.Pool

	Docker *client.Client

	// MaxWait bounds every [Pool.Retry].
	MaxWait time.Duration
}

// NewPool connects to the docker daemon named by the environment.
func NewPool(ctx context.Context, maxWait time.Duration) (*Pool, error) {
	docker, err := client.New(client.FromEnv, client.WithUserAgent("slopscale-integration"))
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	pool, err := dockertest.NewPool(ctx, "", dockertest.WithMobyClient(docker), dockertest.WithMaxWait(maxWait))
	if err != nil {
		return nil, fmt.Errorf("connecting to docker: %w", err)
	}

	return &Pool{Pool: pool, Docker: docker, MaxWait: maxWait}, nil
}

// Retry calls fn every second until it returns nil or [Pool.MaxWait] passes.
func (p *Pool) Retry(fn func() error) error {
	return p.Pool.Retry(context.Background(), p.MaxWait, fn)
}

// Purge stops and removes the container with its anonymous volumes.
func (p *Pool) Purge(resource dockertest.ClosableResource) error {
	return resource.Close(context.Background())
}

// RemoveContainerByName force-removes the container called name, if any.
func (p *Pool) RemoveContainerByName(name string) error {
	ctx := context.Background()

	id, err := lookupContainerID(p, name)
	if errors.Is(err, ErrContainerNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	_, err = p.Docker.ContainerRemove(ctx, id, client.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
	if err != nil {
		return fmt.Errorf("removing container %s: %w", name, err)
	}

	return nil
}

// RestartContainer restarts the container, giving it timeout seconds to
// stop gracefully first.
func (p *Pool) RestartContainer(containerID string, timeout int) error {
	_, err := p.Docker.ContainerRestart(
		context.Background(),
		containerID,
		client.ContainerRestartOptions{Timeout: &timeout},
	)

	return err
}

// RunSpec describes a container the way the harness thinks about it: an
// image, a name, what it runs and which networks it sits on. The first
// network is joined at creation; the others right after start.
type RunSpec struct {
	Name         string
	Repository   string
	Tag          string
	Cmd          []string
	Entrypoint   []string
	Env          []string
	ExposedPorts []string
	PortBindings network.PortMap
	Networks     []*Network
	ExtraHosts   []string
	WorkingDir   string
	Labels       map[string]string
}

// runOptions translates the spec into dockertest's functional options.
// Reuse is off: the harness creates many containers from one image and
// tells them apart by name.
func (s *RunSpec) runOptions(hcOpts []func(*container.HostConfig)) ([]dockertest.RunOption, error) {
	exposed := make(network.PortSet, len(s.ExposedPorts))

	for _, p := range s.ExposedPorts {
		port, err := network.ParsePort(p)
		if err != nil {
			return nil, fmt.Errorf("exposed port %q: %w", p, err)
		}

		exposed[port] = struct{}{}
	}

	opts := []dockertest.RunOption{
		dockertest.WithoutReuse(),
		dockertest.WithName(s.Name),
		dockertest.WithEnv(s.Env),
		dockertest.WithCmd(s.Cmd),
		dockertest.WithEntrypoint(s.Entrypoint),
		dockertest.WithLabels(s.Labels),
		dockertest.WithWorkingDir(s.WorkingDir),
		dockertest.WithContainerConfig(func(cfg *container.Config) {
			if len(exposed) > 0 {
				cfg.ExposedPorts = exposed
			}
		}),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			if len(s.Networks) > 0 {
				hc.NetworkMode = container.NetworkMode(s.Networks[0].Name())
			}

			hc.ExtraHosts = s.ExtraHosts

			if len(s.PortBindings) > 0 {
				hc.PortBindings = s.PortBindings
			}

			for _, opt := range hcOpts {
				opt(hc)
			}
		}),
	}

	if s.Tag != "" {
		opts = append(opts, dockertest.WithTag(s.Tag))
	}

	return opts, nil
}

// Run starts a container from the spec's image, which must be local or
// pullable without credentials (see [PullWithAuth]).
func (p *Pool) Run(spec *RunSpec, hcOpts ...func(*container.HostConfig)) (dockertest.ClosableResource, error) {
	ctx := context.Background()

	opts, err := spec.runOptions(hcOpts)
	if err != nil {
		return nil, err
	}

	resource, err := p.Pool.Run(ctx, spec.Repository, opts...)
	if err != nil {
		return nil, err
	}

	return p.joinRemainingNetworks(ctx, resource, spec)
}

// BuildAndRun builds the image from buildOptions, tags it with the
// container name and runs it.
func (p *Pool) BuildAndRun(
	buildOptions *dockertest.BuildOptions,
	spec *RunSpec,
	hcOpts ...func(*container.HostConfig),
) (dockertest.ClosableResource, error) {
	image := strings.ToLower(spec.Name)

	err := buildImage(context.Background(), p.Docker, buildOptions, image)
	if err != nil {
		return nil, err
	}

	spec.Repository = image
	spec.Tag = "latest"

	return p.Run(spec, hcOpts...)
}

func (p *Pool) joinRemainingNetworks(
	ctx context.Context,
	resource dockertest.ClosableResource,
	spec *RunSpec,
) (dockertest.ClosableResource, error) {
	for _, net := range spec.Networks[min(1, len(spec.Networks)):] {
		err := resource.ConnectToNetwork(ctx, net)
		if err != nil {
			_ = resource.Close(ctx)

			return nil, fmt.Errorf("connecting %s to network %s: %w", spec.Name, net.Name(), err)
		}
	}

	return resource, nil
}
