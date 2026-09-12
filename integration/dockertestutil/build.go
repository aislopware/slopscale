package dockertestutil

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	archive "github.com/moby/go-archive"
	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
	"github.com/moby/patternmatcher/ignorefile"
	"github.com/ory/dockertest/v4"
)

// buildImage builds opts into the image named tag with the classic
// builder, sending the context directory the way docker build does:
// .dockerignore applies, and the Dockerfile itself always ships.
// dockertest's own BuildAndRun tars the whole directory, which at the
// repository root means .git and every node_modules.
func buildImage(ctx context.Context, docker *client.Client, opts *dockertest.BuildOptions, tag string) error {
	dockerfile := cmp.Or(opts.Dockerfile, "Dockerfile")

	excludes, err := readDockerignore(opts.ContextDir)
	if err != nil {
		return err
	}

	excludes = append(excludes, "!"+dockerfile, "!.dockerignore")

	buildContext, err := archive.TarWithOptions(opts.ContextDir, &archive.TarOptions{ExcludePatterns: excludes})
	if err != nil {
		return fmt.Errorf("archiving build context %s: %w", opts.ContextDir, err)
	}
	defer buildContext.Close()

	res, err := docker.ImageBuild(ctx, buildContext, client.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: dockerfile,
		BuildArgs:  opts.BuildArgs,
		NoCache:    opts.NoCache,
		Remove:     true,
		Version:    build.BuilderV1,
	})
	if err != nil {
		return fmt.Errorf("building %s: %w", tag, err)
	}
	defer res.Body.Close()

	err = drainBuildStream(res.Body)
	if err != nil {
		return fmt.Errorf("building %s: %w", tag, err)
	}

	return nil
}

// readDockerignore returns the exclude patterns of the context's
// .dockerignore, or none when there is no such file.
func readDockerignore(contextDir string) ([]string, error) {
	f, err := os.Open(filepath.Join(contextDir, ".dockerignore"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading .dockerignore: %w", err)
	}

	defer f.Close()

	patterns, err := ignorefile.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("parsing .dockerignore: %w", err)
	}

	return patterns, nil
}

// drainBuildStream reads the build output to its end and returns the
// error the daemon embeds in it, since a failed RUN step is reported
// there rather than as an HTTP error.
func drainBuildStream(r io.Reader) error {
	dec := json.NewDecoder(r)

	for dec.More() {
		var msg jsonstream.Message

		err := dec.Decode(&msg)
		if err != nil {
			return fmt.Errorf("decoding build stream: %w", err)
		}

		if msg.Error != nil {
			return msg.Error
		}
	}

	return nil
}

// RunDockerBuildForDiagnostics runs docker build manually to get detailed error output.
// This is used when a docker build fails to provide more detailed diagnostic information
// than what dockertest typically provides.
//
// Returns the build output regardless of success/failure, and an error if the build failed.
func RunDockerBuildForDiagnostics(contextDir, dockerfile string) (string, error) {
	// Use a context with timeout to prevent hanging builds
	const buildTimeout = 10 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "build", "--progress=plain", "--no-cache", "-f", dockerfile, contextDir)
	output, err := cmd.CombinedOutput()

	return string(output), err
}
