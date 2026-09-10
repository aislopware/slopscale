package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aislopware/slopscale/integration/dockertestutil"
	"github.com/cenkalti/backoff/v5"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const defaultDirPerm = 0o755

const (
	// containerFinalizationMaxWait bounds how long waitForContainerFinalization
	// polls before giving up and proceeding with artifact extraction anyway.
	containerFinalizationMaxWait = 10 * time.Second
	// containerFinalizationCheckInterval is the polling interval used by
	// waitForContainerFinalization.
	containerFinalizationCheckInterval = 500 * time.Millisecond
	// imagePullMaxElapsedTime bounds the total retry time for pulling a Docker image.
	imagePullMaxElapsedTime = 60 * time.Second
)

var (
	ErrTestFailed              = errors.New("test failed")
	ErrUnexpectedContainerWait = errors.New("unexpected end of container wait")
	ErrNoDockerContext         = errors.New("no docker context found")
	ErrMemoryLimitViolations   = errors.New("container(s) exceeded memory limits")
)

// runTestContainer executes integration tests in a Docker container.
//
// splitting further would scatter the sequential setup/run/cleanup steps
//
//nolint:gocyclo,gocognit,cyclop,funlen // legacy: orchestrates the full container lifecycle;
func runTestContainer(ctx context.Context, config *RunConfig) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	runID := dockertestutil.GenerateRunID()
	containerName := "slopscale-test-suite-" + runID
	logsDir := filepath.Join(config.LogsDir, runID)

	if config.Verbose {
		log.Printf("Run ID: %s", runID)
		log.Printf("Container name: %s", containerName)
		log.Printf("Logs directory: %s", logsDir)
	}

	absLogsDir, err := filepath.Abs(logsDir)
	if err != nil {
		return fmt.Errorf("getting absolute path for logs directory: %w", err)
	}

	const dirPerm = 0o755

	mkdirErr := os.MkdirAll(absLogsDir, dirPerm)
	if mkdirErr != nil {
		return fmt.Errorf("creating logs directory: %w", mkdirErr)
	}

	if config.CleanBefore {
		if config.Verbose {
			log.Printf("Running pre-test cleanup...")
		}

		cleanupErr := cleanupBeforeTest(ctx)
		if cleanupErr != nil && config.Verbose {
			log.Printf("Warning: pre-test cleanup failed: %v", cleanupErr)
		}
	}

	goTestCmd := buildGoTestCommand(config)
	if config.Verbose {
		log.Printf("Command: %s", strings.Join(goTestCmd, " "))
	}

	imageName := "golang:" + config.GoVersion

	imageErr := ensureImageAvailable(ctx, cli, imageName, config.Verbose)
	if imageErr != nil {
		return fmt.Errorf("ensuring image availability: %w", imageErr)
	}

	resp, err := createGoTestContainer(ctx, cli, config, containerName, absLogsDir, goTestCmd)
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	if config.Verbose {
		log.Printf("Created container: %s", resp.ID)
	}

	_, startErr := cli.ContainerStart(
		ctx,
		resp.ID,
		client.ContainerStartOptions{},
	)
	if startErr != nil {
		return fmt.Errorf("starting container: %w", startErr)
	}

	log.Printf("Starting test: %s", config.TestPattern)
	log.Printf("Run ID: %s", runID)
	log.Printf("Monitor with: docker logs -f %s", containerName)
	log.Printf("Logs directory: %s", logsDir)

	// Start stats collection for container resource monitoring (if enabled)
	statsCollector := startStatsCollector(ctx, config, runID)
	if statsCollector != nil {
		defer statsCollector.Close()
		defer statsCollector.StopCollection()
	}

	exitCode, err := streamAndWait(ctx, cli, resp.ID)

	// Ensure all containers have finished and logs are flushed before extracting artifacts
	waitErr := waitForContainerFinalization(ctx, cli, resp.ID, config.Verbose)
	if waitErr != nil && config.Verbose {
		log.Printf("Warning: failed to wait for container finalization: %v", waitErr)
	}

	// Extract artifacts from test containers before cleanup
	extractErr := extractArtifactsFromContainers(
		ctx,
		resp.ID,
		logsDir,
		config.Verbose,
	)
	if extractErr != nil && config.Verbose {
		log.Printf("Warning: failed to extract artifacts from containers: %v", extractErr)
	}

	// Always list control files regardless of test outcome
	listControlFiles(logsDir)

	// Print stats summary and check memory limits if enabled
	if config.Stats && statsCollector != nil {
		violations := statsCollector.PrintSummaryAndCheckLimits(config.HSMemoryLimit, config.TSMemoryLimit)
		if len(violations) > 0 {
			log.Printf("MEMORY LIMIT VIOLATIONS DETECTED:")
			log.Printf("=================================")

			for _, violation := range violations {
				log.Printf("Container %s exceeded memory limit: %.1f MB > %.1f MB",
					violation.ContainerName, violation.MaxMemoryMB, violation.LimitMB)
			}

			return fmt.Errorf("test failed: %d %w", len(violations), ErrMemoryLimitViolations)
		}
	}

	shouldCleanup := config.CleanAfter && (!config.KeepOnFailure || exitCode == 0)
	if shouldCleanup {
		if config.Verbose {
			log.Printf("Running post-test cleanup for run %s...", runID)
		}

		cleanErr := cleanupAfterTest(ctx, cli, resp.ID, runID)

		if cleanErr != nil && config.Verbose {
			log.Printf("Warning: post-test cleanup failed: %v", cleanErr)
		}

		// Clean up artifacts from successful tests to save disk space in CI
		if exitCode == 0 {
			if config.Verbose {
				log.Printf("Test succeeded, cleaning up artifacts to save disk space...")
			}

			cleanErr := cleanupSuccessfulTestArtifacts(logsDir, config.Verbose)

			if cleanErr != nil && config.Verbose {
				log.Printf("Warning: artifact cleanup failed: %v", cleanErr)
			}
		}
	}

	if err != nil {
		return fmt.Errorf("executing test: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("%w: exit code %d", ErrTestFailed, exitCode)
	}

	log.Printf("Test completed successfully!")

	return nil
}

// startStatsCollector creates and starts a [StatsCollector] for the given run
// when config.Stats is enabled. It returns nil when stats collection is
// disabled or fails to initialize or start; the caller must call Close and
// StopCollection on a non-nil result.
func startStatsCollector(ctx context.Context, config *RunConfig, runID string) *StatsCollector {
	if !config.Stats {
		return nil
	}

	statsCollector, err := NewStatsCollector(ctx)
	if err != nil {
		if config.Verbose {
			log.Printf("Warning: failed to create stats collector: %v", err)
		}

		return nil
	}

	// Start stats collection immediately - no need for complex retry logic.
	// The new implementation monitors Docker events and will catch containers as they start.
	startErr := statsCollector.StartCollection(ctx, runID, config.Verbose)
	if startErr != nil {
		if config.Verbose {
			log.Printf("Warning: failed to start stats collection: %v", startErr)
		}
	}

	return statsCollector
}

// buildGoTestCommand constructs the go test command arguments.
func buildGoTestCommand(config *RunConfig) []string {
	// Only the integration package holds tests this runner can drive. The
	// subpackages hold helpers plus a couple of unit tests that `make test`
	// already runs, and building them here costs a compile and a link per
	// invocation for a "no tests to run" line.
	cmd := []string{"go", "test", "."}

	if config.TestPattern != "" {
		cmd = append(cmd, "-run", config.TestPattern)
	}

	if config.FailFast {
		cmd = append(cmd, "-failfast")
	}

	cmd = append(cmd, "-timeout", config.Timeout.String(), "-v")

	return cmd
}

// createGoTestContainer creates a Docker container configured for running integration tests.
func createGoTestContainer(
	ctx context.Context,
	cli *client.Client,
	config *RunConfig,
	containerName, logsDir string,
	goTestCmd []string,
) (client.ContainerCreateResult, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return client.ContainerCreateResult{}, fmt.Errorf("getting working directory: %w", err)
	}

	projectRoot := findProjectRoot(pwd)

	runID := dockertestutil.ExtractRunIDFromContainerName(containerName)

	env := []string{
		fmt.Sprintf("SLOPSCALE_INTEGRATION_POSTGRES=%d", boolToInt(config.UsePostgres)),
		"SLOPSCALE_INTEGRATION_RUN_ID=" + runID,
	}

	// Pass through CI environment variable for CI detection
	if ci := os.Getenv("CI"); ci != "" {
		env = append(env, "CI="+ci)
	}

	// Pass through all SLOPSCALE_INTEGRATION_* environment variables
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "SLOPSCALE_INTEGRATION_") {
			// Skip the ones we already set explicitly
			if strings.HasPrefix(e, "SLOPSCALE_INTEGRATION_POSTGRES=") ||
				strings.HasPrefix(e, "SLOPSCALE_INTEGRATION_RUN_ID=") {
				continue
			}

			env = append(env, e)
		}
	}

	// Set GOCACHE to a known location (used by both bind mount and volume cases)
	env = append(env, "GOCACHE=/cache/go-build")

	containerConfig := &container.Config{
		Image:      "golang:" + config.GoVersion,
		Cmd:        goTestCmd,
		Env:        env,
		WorkingDir: projectRoot + "/integration",
		Tty:        true,
		Labels: map[string]string{
			"hi.run-id":    runID,
			"hi.test-type": "test-runner",
		},
	}

	// Get the correct Docker socket path from the current context
	dockerSocketPath := getDockerSocketPath()

	if config.Verbose {
		log.Printf("Using Docker socket: %s", dockerSocketPath)
	}

	binds := []string{
		fmt.Sprintf("%s:%s", projectRoot, projectRoot),
		dockerSocketPath + ":/var/run/docker.sock",
		logsDir + ":/tmp/control",
	}

	// Use bind mounts for Go cache if provided via environment variables,
	// otherwise fall back to Docker volumes for local development
	var mounts []mount.Mount

	goCache := os.Getenv("SLOPSCALE_INTEGRATION_GO_CACHE")
	goBuildCache := os.Getenv("SLOPSCALE_INTEGRATION_GO_BUILD_CACHE")

	if goCache != "" {
		binds = append(binds, goCache+":/go")
	} else {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: "hs-integration-go-cache",
			Target: "/go",
		})
	}

	if goBuildCache != "" {
		binds = append(binds, goBuildCache+":/cache/go-build")
	} else {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: "hs-integration-go-build-cache",
			Target: "/cache/go-build",
		})
	}

	hostConfig := &container.HostConfig{
		AutoRemove: false, // We'll remove manually for better control
		Binds:      binds,
		Mounts:     mounts,
	}

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     containerConfig,
		HostConfig: hostConfig,
		Name:       containerName,
	})
	if err != nil {
		return resp, fmt.Errorf("creating container %s: %w", containerName, err)
	}

	return resp, nil
}

// streamAndWait streams container output and waits for completion.
func streamAndWait(ctx context.Context, cli *client.Client, containerID string) (int, error) {
	out, err := cli.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return -1, fmt.Errorf("getting container logs: %w", err)
	}
	defer out.Close()

	go func() {
		_, _ = io.Copy(os.Stdout, out)
	}()

	waitResult := cli.ContainerWait(ctx, containerID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})
	select {
	case err := <-waitResult.Error:
		if err != nil {
			return -1, fmt.Errorf("waiting for container: %w", err)
		}
	case status := <-waitResult.Result:
		return int(status.StatusCode), nil
	}

	return -1, ErrUnexpectedContainerWait
}

// waitForContainerFinalization ensures all test containers have properly finished and flushed their output.
func waitForContainerFinalization(ctx context.Context, cli *client.Client, testContainerID string, verbose bool) error {
	// First, get all related test containers
	listResult, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	testContainers := getCurrentTestContainers(listResult.Items, testContainerID, verbose)

	// Wait for all test containers to reach a final state
	timeout := time.After(containerFinalizationMaxWait)

	ticker := time.NewTicker(containerFinalizationCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			if verbose {
				log.Printf("Timeout waiting for container finalization, proceeding with artifact extraction")
			}

			return nil
		case <-ticker.C:
			if allTestContainersFinalized(ctx, cli, testContainers, verbose) {
				if verbose {
					log.Printf("All test containers finalized, ready for artifact extraction")
				}

				return nil
			}
		}
	}
}

// allTestContainersFinalized reports whether every container in testContainers
// has reached a final state (not running, with a finish time).
func allTestContainersFinalized(
	ctx context.Context,
	cli *client.Client,
	testContainers []testContainer,
	verbose bool,
) bool {
	for _, testCont := range testContainers {
		inspect, err := cli.ContainerInspect(ctx, testCont.ID, client.ContainerInspectOptions{})
		if err != nil {
			if verbose {
				log.Printf("Warning: failed to inspect container %s: %v", testCont.name, err)
			}

			continue
		}

		if !isContainerFinalized(inspect.Container.State) {
			if verbose {
				log.Printf("Container %s still finalizing (state: %s)", testCont.name, inspect.Container.State.Status)
			}

			return false
		}
	}

	return true
}

// isContainerFinalized checks if a container has reached a final state where logs are flushed.
func isContainerFinalized(state *container.State) bool {
	// Container is finalized if it's not running and has a finish time
	return !state.Running && state.FinishedAt != ""
}

// findProjectRoot locates the project root by finding the directory containing go.mod.
func findProjectRoot(startPath string) string {
	current := startPath
	for {
		_, err := os.Stat(filepath.Join(current, "go.mod"))
		if err == nil {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			return startPath
		}

		current = parent
	}
}

// boolToInt converts a boolean to an integer for environment variables.
func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// DockerContext represents Docker context information.
type DockerContext struct {
	Name      string         `json:"Name"`
	Metadata  map[string]any `json:"Metadata"`
	Endpoints map[string]any `json:"Endpoints"`
	Current   bool           `json:"Current"`
}

// createDockerClient creates a Docker client with context detection.
func createDockerClient(ctx context.Context) (*client.Client, error) {
	contextInfo, err := getCurrentDockerContext(ctx)
	if err != nil {
		cli, clientErr := client.New(client.FromEnv, client.WithUserAgent("slopscale-hi"))
		if clientErr != nil {
			return nil, fmt.Errorf("creating Docker client from environment: %w", clientErr)
		}

		return cli, nil
	}

	var clientOpts []client.Opt

	if host, ok := dockerHostFromContext(contextInfo); ok {
		if runConfig.Verbose {
			log.Printf("Using Docker host from context '%s': %s", contextInfo.Name, host)
		}

		clientOpts = append(clientOpts, client.WithHost(host))
	}

	if len(clientOpts) == 0 {
		clientOpts = append(clientOpts, client.FromEnv)
	}

	clientOpts = append(clientOpts, client.WithUserAgent("slopscale-hi"))

	cli, err := client.New(clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating Docker client: %w", err)
	}

	return cli, nil
}

// dockerHostFromContext extracts the Docker host endpoint from context
// metadata, if present.
func dockerHostFromContext(contextInfo *DockerContext) (string, bool) {
	if contextInfo == nil {
		return "", false
	}

	endpoints, ok := contextInfo.Endpoints["docker"]
	if !ok {
		return "", false
	}

	endpointMap, ok := endpoints.(map[string]any)
	if !ok {
		return "", false
	}

	host, ok := endpointMap["Host"].(string)
	if !ok {
		return "", false
	}

	return host, true
}

// getCurrentDockerContext retrieves the current Docker context information.
func getCurrentDockerContext(ctx context.Context) (*DockerContext, error) {
	cmd := exec.CommandContext(ctx, "docker", "context", "inspect")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting docker context: %w", err)
	}

	var contexts []DockerContext

	err = json.Unmarshal(output, &contexts)
	if err != nil {
		return nil, fmt.Errorf("parsing docker context: %w", err)
	}

	if len(contexts) > 0 {
		return &contexts[0], nil
	}

	return nil, ErrNoDockerContext
}

// getDockerSocketPath returns the correct Docker socket path for the current context.
func getDockerSocketPath() string {
	// Always use the default socket path for mounting since Docker handles
	// the translation to the actual socket (e.g., colima socket) internally
	return "/var/run/docker.sock"
}

// checkImageAvailableLocally checks if the specified Docker image is available locally.
func checkImageAvailableLocally(ctx context.Context, cli *client.Client, imageName string) (bool, error) {
	_, err := cli.ImageInspect(ctx, imageName)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("inspecting image %s: %w", imageName, err)
	}

	return true, nil
}

// ensureImageAvailable pulls imageName if missing, using Docker Hub
// credentials and retrying transient errors.
func ensureImageAvailable(ctx context.Context, cli *client.Client, imageName string, verbose bool) error {
	available, err := checkImageAvailableLocally(ctx, cli, imageName)
	if err != nil {
		return fmt.Errorf("checking local image availability: %w", err)
	}

	if available {
		if verbose {
			log.Printf("Image %s is available locally", imageName)
		}

		return nil
	}

	if verbose {
		log.Printf("Image %s not found locally, pulling...", imageName)
	}

	registryAuth, err := dockertestutil.RegistryAuth()
	if err != nil {
		return fmt.Errorf("resolving registry auth: %w", err)
	}

	_, err = backoff.Retry(
		ctx,
		func() (struct{}, error) {
			reader, pullErr := cli.ImagePull(ctx, imageName, client.ImagePullOptions{RegistryAuth: registryAuth})
			if pullErr != nil {
				if isPermanentDockerPullError(pullErr) {
					return struct{}{}, backoff.Permanent(pullErr)
				}

				return struct{}{}, fmt.Errorf("pulling image %s: %w", imageName, pullErr)
			}
			defer reader.Close()

			sink := io.Discard
			if verbose {
				sink = os.Stdout
			}

			_, copyErr := io.Copy(sink, reader)
			if copyErr != nil {
				return struct{}{}, fmt.Errorf("reading pull output: %w", copyErr)
			}

			return struct{}{}, nil
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxElapsedTime(imagePullMaxElapsedTime),
	)
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}

	if !verbose {
		log.Printf("Image %s pulled successfully", imageName)
	}

	return nil
}

func isPermanentDockerPullError(err error) bool {
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "manifest unknown") ||
		strings.Contains(msg, "manifest not found") ||
		strings.Contains(msg, "repository does not exist") ||
		strings.Contains(msg, "name unknown") ||
		strings.Contains(msg, "no such image")
}

// listControlFiles displays the slopscale test artifacts created in the control logs directory.
func listControlFiles(logsDir string) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		log.Printf("Logs directory: %s", logsDir)
		return
	}

	var (
		logFiles  []string
		dataFiles []string
		dataDirs  []string
	)

	for _, entry := range entries {
		name := entry.Name()
		// Only show slopscale (hs-*) files and directories
		if !strings.HasPrefix(name, "hs-") {
			continue
		}

		if entry.IsDir() {
			// Include directories (pprof, mapresponses)
			if strings.Contains(name, "-pprof") || strings.Contains(name, "-mapresponses") {
				dataDirs = append(dataDirs, name)
			}
		} else {
			// Include files
			switch {
			case strings.HasSuffix(name, ".stderr.log") || strings.HasSuffix(name, ".stdout.log"):
				logFiles = append(logFiles, name)
			case strings.HasSuffix(name, ".db"):
				dataFiles = append(dataFiles, name)
			}
		}
	}

	log.Printf("Test artifacts saved to: %s", logsDir)

	if len(logFiles) > 0 {
		log.Printf("Slopscale logs:")

		for _, file := range logFiles {
			log.Printf("  %s", file)
		}
	}

	if len(dataFiles) > 0 || len(dataDirs) > 0 {
		log.Printf("Slopscale data:")

		for _, file := range dataFiles {
			log.Printf("  %s", file)
		}

		for _, dir := range dataDirs {
			log.Printf("  %s/", dir)
		}
	}
}

// extractArtifactsFromContainers collects container logs and files from the specific test run.
func extractArtifactsFromContainers(ctx context.Context, testContainerID, logsDir string, verbose bool) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	// List all containers
	listResult, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	// Get containers from the specific test run
	currentTestContainers := getCurrentTestContainers(listResult.Items, testContainerID, verbose)

	extractedCount := 0

	for _, cont := range currentTestContainers {
		// Extract container logs and tar files
		err := extractContainerArtifacts(ctx, cli, cont.ID, cont.name, logsDir, verbose)
		if err != nil {
			if verbose {
				log.Printf(
					"Warning: failed to extract artifacts from container %s (%s): %v",
					cont.name,
					cont.ID[:12],
					err,
				)
			}
		} else {
			if verbose {
				log.Printf("Extracted artifacts from container %s (%s)", cont.name, cont.ID[:12])
			}

			extractedCount++
		}
	}

	if verbose && extractedCount > 0 {
		log.Printf("Extracted artifacts from %d containers", extractedCount)
	}

	return nil
}

// testContainer represents a container from the current test run.
type testContainer struct {
	ID   string
	name string
}

// getCurrentTestContainers filters containers to only include those from the current test run.
func getCurrentTestContainers(containers []container.Summary, testContainerID string, verbose bool) []testContainer {
	var testRunContainers []testContainer

	// Find the test container to get its run ID label
	var runID string

	for _, cont := range containers {
		if cont.ID == testContainerID {
			if cont.Labels != nil {
				runID = cont.Labels["hi.run-id"]
			}

			break
		}
	}

	if runID == "" {
		log.Printf("Error: test container %s missing required hi.run-id label", testContainerID[:12])
		return testRunContainers
	}

	if verbose {
		log.Printf("Looking for containers with run ID: %s", runID)
	}

	// Find all containers with the same run ID
	for _, cont := range containers {
		for _, name := range cont.Names {
			containerName := strings.TrimPrefix(name, "/")
			if matchesTestContainerPrefix(containerName) {
				// Check if container has matching run ID label
				if cont.Labels != nil && cont.Labels["hi.run-id"] == runID {
					testRunContainers = append(testRunContainers, testContainer{
						ID:   cont.ID,
						name: containerName,
					})
					if verbose {
						log.Printf("Including container %s (run ID: %s)", containerName, runID)
					}
				}

				break
			}
		}
	}

	return testRunContainers
}

// extractContainerArtifacts saves logs and tar files from a container.
func extractContainerArtifacts(
	ctx context.Context,
	cli *client.Client,
	containerID, containerName, logsDir string,
	verbose bool,
) error {
	// Ensure the logs directory exists
	err := os.MkdirAll(logsDir, defaultDirPerm)
	if err != nil {
		return fmt.Errorf("creating logs directory: %w", err)
	}

	// Extract container logs
	err = extractContainerLogs(ctx, cli, containerID, containerName, logsDir, verbose)
	if err != nil {
		return fmt.Errorf("extracting logs: %w", err)
	}

	return nil
}

// extractContainerLogs saves the stdout and stderr logs from a container to files.
func extractContainerLogs(
	ctx context.Context,
	cli *client.Client,
	containerID, containerName, logsDir string,
	verbose bool,
) error {
	// Get container logs
	logReader, err := cli.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Follow:     false,
		Tail:       "all",
	})
	if err != nil {
		return fmt.Errorf("getting container logs: %w", err)
	}
	defer logReader.Close()

	// Create log files following the slopscale naming convention
	stdoutPath := filepath.Join(logsDir, containerName+".stdout.log")
	stderrPath := filepath.Join(logsDir, containerName+".stderr.log")

	// Create buffers to capture stdout and stderr separately
	var stdoutBuf, stderrBuf bytes.Buffer

	// Demultiplex the Docker logs stream to separate stdout and stderr
	_, err = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, logReader)
	if err != nil {
		return fmt.Errorf("demultiplexing container logs: %w", err)
	}

	// Write stdout logs
	stdoutErr := os.WriteFile(
		stdoutPath,
		stdoutBuf.Bytes(),
		0o600,
	)
	if stdoutErr != nil {
		return fmt.Errorf("writing stdout log: %w", stdoutErr)
	}

	// Write stderr logs
	stderrErr := os.WriteFile(
		stderrPath,
		stderrBuf.Bytes(),
		0o600,
	)
	if stderrErr != nil {
		return fmt.Errorf("writing stderr log: %w", stderrErr)
	}

	if verbose {
		log.Printf("Saved logs for %s: %s, %s", containerName, stdoutPath, stderrPath)
	}

	return nil
}
