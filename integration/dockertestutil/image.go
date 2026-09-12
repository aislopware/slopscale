package dockertestutil

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"
)

var (
	// ErrInvalidImageFormat reports an image variable that is set but does
	// not name a repository and a tag.
	ErrInvalidImageFormat = errors.New("invalid image format, expected repository:tag")

	// ErrImageRequiredInCI reports a missing image variable in CI, where
	// building instead would be a silent regression rather than a failure.
	ErrImageRequiredInCI = errors.New("pre-built image variable must be set in CI")
)

// RunPrebuiltOrBuild starts spec from the image named by env, and
// builds buildOptions only when that variable is empty.
//
// CI builds every image once and passes it to the test jobs, so a build
// there means the variable was lost and each test that reaches this would
// pay the build again. That is an error rather than a slow success, the
// same rule hsic and tsic apply to their own images.
func RunPrebuiltOrBuild(
	pool *Pool,
	env string,
	buildOptions *dockertest.BuildOptions,
	spec *RunSpec,
	hcOpts ...func(*container.HostConfig),
) (dockertest.ClosableResource, error) {
	image := os.Getenv(env)

	switch {
	case image != "":
		repo, tag, ok := strings.Cut(image, ":")
		if !ok {
			return nil, fmt.Errorf("%w: %s=%q", ErrInvalidImageFormat, env, image)
		}

		log.Printf("Using pre-built image %s for %s", image, spec.Name)

		spec.Repository = repo
		spec.Tag = tag

		resource, err := pool.Run(spec, hcOpts...)
		if err != nil {
			return nil, fmt.Errorf("running %s from pre-built image %q: %w", spec.Name, image, err)
		}

		return resource, nil

	case util.IsCI():
		return nil, fmt.Errorf("%w: %s", ErrImageRequiredInCI, env)

	default:
		resource, err := pool.BuildAndRun(buildOptions, spec, hcOpts...)
		if err != nil {
			return nil, fmt.Errorf("building and running %s: %w", spec.Name, err)
		}

		return resource, nil
	}
}
