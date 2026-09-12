package dockertestutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/ory/dockertest/v4"
)

// defaultExecuteTimeout returns the timeout for docker exec commands.
// On CI runners, docker exec latency is higher due to resource
// contention, so the timeout is doubled.
func defaultExecuteTimeout() time.Duration {
	if util.IsCI() {
		return 20 * time.Second
	}

	return 10 * time.Second
}

var (
	ErrDockertestCommandFailed  = errors.New("dockertest command failed")
	ErrDockertestCommandTimeout = errors.New("dockertest command timed out")
)

type ExecuteCommandConfig struct {
	timeout time.Duration
}

type ExecuteCommandOption func(*ExecuteCommandConfig) error

func ExecuteCommandTimeout(timeout time.Duration) ExecuteCommandOption {
	return ExecuteCommandOption(func(conf *ExecuteCommandConfig) error {
		conf.timeout = timeout
		return nil
	})
}

// ExecuteCommand runs cmd in the container with env and returns its
// stdout and stderr. A non-zero exit or the timeout is an error.
func ExecuteCommand(
	pool *Pool,
	resource dockertest.Resource,
	cmd []string,
	env []string,
	options ...ExecuteCommandOption,
) (string, string, error) {
	var stdout, stderr bytes.Buffer

	execConfig := ExecuteCommandConfig{
		timeout: defaultExecuteTimeout(),
	}

	for _, opt := range options {
		err := opt(&execConfig)
		if err != nil {
			return "", "", fmt.Errorf("execute-command/options: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), execConfig.timeout)
	defer cancel()

	exitCode, err := execInContainer(
		ctx,
		pool.Docker,
		resource.ID(),
		cmd,
		append(env, "SLOPSCALE_LOG_LEVEL=info"),
		&stdout,
		&stderr,
	)

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return stdout.String(), stderr.String(), fmt.Errorf(
			"command failed, stderr: %s: %w",
			stderr.String(),
			ErrDockertestCommandTimeout,
		)
	case err != nil:
		return stdout.String(), stderr.String(), fmt.Errorf(
			"command failed, stderr: %s: %w",
			stderr.String(),
			err,
		)
	case exitCode != 0:
		return stdout.String(), stderr.String(), fmt.Errorf(
			"command failed, stderr: %s: %w",
			stderr.String(),
			ErrDockertestCommandFailed,
		)
	}

	return stdout.String(), stderr.String(), nil
}
