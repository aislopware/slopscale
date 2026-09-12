package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v7"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// cleanupBeforeTest performs cleanup operations before running tests.
// Only removes stale (stopped/exited) test containers to avoid interfering with concurrent test runs.
func cleanupBeforeTest(ctx context.Context) error {
	err := cleanupStaleTestContainers(ctx)
	if err != nil {
		return fmt.Errorf("cleaning stale test containers: %w", err)
	}

	err = pruneDockerNetworks(ctx, stalePruneAge)
	if err != nil {
		return fmt.Errorf("pruning networks: %w", err)
	}

	return nil
}

// cleanupAfterTest removes the test container and all associated integration test containers for the run.
func cleanupAfterTest(ctx context.Context, cli *client.Client, containerID, runID string) error {
	// Remove the main test container
	_, err := cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("removing test container: %w", err)
	}

	// Clean up integration test containers for this run only
	if runID != "" {
		err := killTestContainersByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("cleaning up containers for run %s: %w", runID, err)
		}
	}

	return nil
}

// killTestContainers terminates and removes all test containers.
func killTestContainers(ctx context.Context) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	listResult, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	removed := 0

	for _, cont := range listResult.Items {
		if isTestContainerName(cont.Names) {
			if killAndRemove(ctx, cli, cont) {
				removed++
			}
		}
	}

	if removed > 0 {
		fmt.Printf("Removed %d test containers\n", removed)
	} else {
		fmt.Println("No test containers found to remove")
	}

	return nil
}

// killTestContainersByRunID terminates and removes all test containers for a specific run ID.
// This function filters containers by the hi.run-id label to only affect containers
// belonging to the specified test run, leaving other concurrent test runs untouched.
func killTestContainersByRunID(ctx context.Context, runID string) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	// Filter containers by hi.run-id label
	listResult, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", "hi.run-id="+runID),
	})
	if err != nil {
		return fmt.Errorf("listing containers for run %s: %w", runID, err)
	}

	removed := 0

	for _, cont := range listResult.Items {
		if killAndRemove(ctx, cli, cont) {
			removed++
		}
	}

	if removed > 0 {
		fmt.Printf("Removed %d containers for run ID %s\n", removed, runID)
	}

	return nil
}

// cleanupStaleTestContainers removes stopped/exited test containers without affecting running tests.
// This is useful for cleaning up leftover containers from previous crashed or interrupted test runs
// without interfering with currently running concurrent tests.
func cleanupStaleTestContainers(ctx context.Context) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	// Only get stopped/exited containers
	listResult, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("status", "exited", "dead"),
	})
	if err != nil {
		return fmt.Errorf("listing stopped containers: %w", err)
	}

	removed := 0

	for _, cont := range listResult.Items {
		// Only remove containers that look like test containers
		if isTestContainerName(cont.Names) {
			if killAndRemove(ctx, cli, cont) {
				removed++
			}
		}
	}

	if removed > 0 {
		fmt.Printf("Removed %d stale test containers\n", removed)
	}

	return nil
}

const (
	containerRemoveInitialInterval = 100 * time.Millisecond
	containerRemoveMaxElapsedTime  = 2 * time.Second

	// stalePruneAge is how old an unused resource must be before
	// `--clean-before` will remove it. Anything younger may belong to a run
	// that started moments ago beside this one.
	stalePruneAge = 2 * time.Hour
)

// removeContainerWithRetry attempts to remove a container with exponential backoff retry logic.
func removeContainerWithRetry(ctx context.Context, cli *client.Client, containerID string) bool {
	expBackoff := backoff.NewExponentialBackOff()
	expBackoff.InitialInterval = containerRemoveInitialInterval

	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		_, err := cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
			Force: true,
		})
		if err != nil {
			return struct{}{}, fmt.Errorf("removing container %s: %w", containerID, err)
		}

		return struct{}{}, nil
	}, backoff.WithBackOff(expBackoff), backoff.WithMaxElapsedTime(containerRemoveMaxElapsedTime))

	return err == nil
}

// testContainerNamePrefixes are the name prefixes used by containers that the
// integration test harness creates (slopscale, tailscale, DERP, and k3s).
var testContainerNamePrefixes = []string{"hs-", "ts-", "derp-", "k3s-"}

// matchesTestContainerPrefix reports whether name belongs to an integration
// test container, ignoring any leading "/" that Docker prefixes names with.
func matchesTestContainerPrefix(name string) bool {
	name = strings.TrimPrefix(name, "/")
	for _, prefix := range testContainerNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// isTestContainerName reports whether any of the container names belong to an
// integration test container.
func isTestContainerName(names []string) bool {
	for _, name := range names {
		if strings.Contains(name, "slopscale-test-suite") ||
			matchesTestContainerPrefix(name) {
			return true
		}
	}

	return false
}

// killAndRemove kills a running container then removes it with retry logic,
// reporting whether the removal succeeded.
func killAndRemove(ctx context.Context, cli *client.Client, cont container.Summary) bool {
	if cont.State == "running" {
		_, _ = cli.ContainerKill(ctx, cont.ID, client.ContainerKillOptions{Signal: "KILL"})
	}

	return removeContainerWithRetry(ctx, cli, cont.ID)
}

// pruneDockerNetworks removes unused Docker networks. A non-zero olderThan
// spares networks younger than it.
func pruneDockerNetworks(ctx context.Context, olderThan time.Duration) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	// An unfiltered prune removes every unused network on the daemon, and a
	// scenario that has created its network but not yet attached a
	// container to it looks exactly like one. Runs share a daemon, so the
	// automatic sweep passes an age that no live run can be older than;
	// `hi clean networks` passes zero and prunes the lot.
	filters := make(client.Filters)
	if olderThan > 0 {
		filters.Add("until", olderThan.String())
	}

	pruneResult, err := cli.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: filters})
	if err != nil {
		return fmt.Errorf("pruning networks: %w", err)
	}

	if len(pruneResult.Report.NetworksDeleted) > 0 {
		fmt.Printf("Removed %d unused networks\n", len(pruneResult.Report.NetworksDeleted))
	} else {
		fmt.Println("No unused networks found to remove")
	}

	return nil
}

// cleanOldImages removes test-related and old dangling Docker images.
func cleanOldImages(ctx context.Context) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	listResult, err := cli.ImageList(ctx, client.ImageListOptions{
		All: true,
	})
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}

	removed := 0

	for _, img := range listResult.Items {
		shouldRemove := false

		for _, tag := range img.RepoTags {
			if strings.Contains(tag, "hs-") ||
				strings.Contains(tag, "slopscale-integration") ||
				strings.Contains(tag, "tailscale") {
				shouldRemove = true
				break
			}
		}

		if len(img.RepoTags) == 0 && time.Unix(img.Created, 0).Before(time.Now().Add(-7*24*time.Hour)) {
			shouldRemove = true
		}

		if shouldRemove {
			_, err := cli.ImageRemove(ctx, img.ID, client.ImageRemoveOptions{
				Force: true,
			})
			if err == nil {
				removed++
			}
		}
	}

	if removed > 0 {
		fmt.Printf("Removed %d test images\n", removed)
	} else {
		fmt.Println("No test images found to remove")
	}

	return nil
}

// cleanCacheVolume removes the Docker volume used for Go module cache.
func cleanCacheVolume(ctx context.Context) error {
	cli, err := createDockerClient(ctx)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer cli.Close()

	volumeName := "hs-integration-go-cache"

	_, err = cli.VolumeRemove(ctx, volumeName, client.VolumeRemoveOptions{Force: true})
	if err != nil {
		switch {
		case cerrdefs.IsNotFound(err):
			fmt.Printf("Go module cache volume not found: %s\n", volumeName)
		case cerrdefs.IsConflict(err):
			fmt.Printf("Go module cache volume is in use and cannot be removed: %s\n", volumeName)
		default:
			fmt.Printf("Failed to remove Go module cache volume %s: %v\n", volumeName, err)
		}
	} else {
		fmt.Printf("Removed Go module cache volume: %s\n", volumeName)
	}

	return nil
}

// cleanupSuccessfulTestArtifacts removes artifacts from successful test runs to save disk space.
// This function removes large artifacts that are mainly useful for debugging failures:
// - Database dumps (.db files)
// - Profile data (pprof directories)
// - MapResponse data (mapresponses directories)
// - Prometheus metrics files
//
// It preserves:
// - Log files (.log) which are small and useful for verification.
func cleanupSuccessfulTestArtifacts(logsDir string, verbose bool) error {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return fmt.Errorf("reading logs directory: %w", err)
	}

	var (
		removedFiles, removedDirs int
		totalSize                 int64
	)

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(logsDir, name)

		if entry.IsDir() {
			removed, size := removeSuccessfulTestArtifactDir(fullPath, name, verbose)
			if removed {
				removedDirs++
				totalSize += size
			}

			continue
		}

		removed, size := removeSuccessfulTestArtifactFile(entry, fullPath, name, verbose)
		if removed {
			removedFiles++
			totalSize += size
		}
	}

	if removedFiles > 0 || removedDirs > 0 {
		const bytesPerMB = 1024 * 1024
		log.Printf("Cleaned up %d files and %d directories (freed ~%.2f MB)",
			removedFiles, removedDirs, float64(totalSize)/bytesPerMB)
	}

	return nil
}

// removeSuccessfulTestArtifactDir removes fullPath if name is one of the
// large per-run artifact directories (pprof, mapresponses), reporting
// whether it was removed and its size before removal.
func removeSuccessfulTestArtifactDir(fullPath, name string, verbose bool) (bool, int64) {
	// Remove pprof and mapresponses directories (typically large).
	// These directories contain artifacts from all containers in the test run.
	if name != "pprof" && name != "mapresponses" {
		return false, 0
	}

	size, sizeErr := getDirSize(fullPath)
	if sizeErr != nil {
		size = 0
	}

	err := os.RemoveAll(fullPath)
	if err != nil {
		if verbose {
			log.Printf("Warning: failed to remove directory %s: %v", name, err)
		}

		return false, 0
	}

	if verbose {
		log.Printf("Removed directory: %s/", name)
	}

	return true, size
}

// removeSuccessfulTestArtifactFile removes fullPath if name is a slopscale
// or tailscale database, metrics, or status file, reporting whether it was
// removed and its size before removal. Log files are always kept.
func removeSuccessfulTestArtifactFile(entry os.DirEntry, fullPath, name string, verbose bool) (bool, int64) {
	// Only process test-related files (slopscale and tailscale).
	if !strings.HasPrefix(name, "hs-") && !strings.HasPrefix(name, "ts-") {
		return false, 0
	}

	// Remove database, metrics, and status files, but keep logs.
	shouldRemove := strings.HasSuffix(name, ".db") ||
		strings.HasSuffix(name, "_metrics.txt") ||
		strings.HasSuffix(name, "_status.json")
	if !shouldRemove {
		return false, 0
	}

	var size int64

	info, infoErr := entry.Info()
	if infoErr == nil {
		size = info.Size()
	}

	err := os.Remove(fullPath)
	if err != nil {
		if verbose {
			log.Printf("Warning: failed to remove file %s: %v", name, err)
		}

		return false, 0
	}

	if verbose {
		log.Printf("Removed file: %s", name)
	}

	return true, size
}

// getDirSize calculates the total size of a directory.
func getDirSize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			size += info.Size()
		}

		return nil
	})
	if err != nil {
		return size, fmt.Errorf("walking directory %s: %w", path, err)
	}

	return size, nil
}
