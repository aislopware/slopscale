package v2

import (
	"path/filepath"
	"runtime"
	"sync"

	"github.com/juanfont/headscale/hscontrol/types/testcapture"
)

// The compat tests replay several hundred megabytes of captured Tailscale
// data. Parsing a file in the parent test before each parallel subtest made
// the parent the bottleneck, and the routes tests parsed the same directory
// once per test. readCapture parses each file once per test binary and, the
// first time a directory is touched, parses every capture in it on all CPUs
// so the sequential loops that follow find their files ready.

type captureEntry struct {
	once sync.Once
	c    *testcapture.Capture
	err  error
}

func (e *captureEntry) load(path string) {
	e.once.Do(func() {
		e.c, e.err = testcapture.Read(path)
	})
}

var captureCache = struct {
	mu      sync.Mutex
	entries map[string]*captureEntry
	dirs    map[string]bool
}{
	entries: map[string]*captureEntry{},
	dirs:    map[string]bool{},
}

// readCapture returns the parsed capture at path, sharing one parse across
// every caller. Captures are read-only fixtures; callers must not mutate them.
func readCapture(path string) (*testcapture.Capture, error) {
	captureCache.mu.Lock()

	if dir := filepath.Dir(path); !captureCache.dirs[dir] {
		captureCache.dirs[dir] = true
		prefetchCapturesLocked(dir)
	}

	entry := captureCache.entries[path]
	if entry == nil {
		entry = &captureEntry{}
		captureCache.entries[path] = entry
	}

	captureCache.mu.Unlock()

	entry.load(path)

	return entry.c, entry.err
}

// prefetchCapturesLocked parses every capture in dir in the background,
// bounded to one parse per CPU. captureCache.mu must be held.
func prefetchCapturesLocked(dir string) {
	files, err := filepath.Glob(filepath.Join(dir, "*.hujson"))
	if err != nil {
		return
	}

	sem := make(chan struct{}, runtime.GOMAXPROCS(0))

	for _, file := range files {
		entry := &captureEntry{}
		captureCache.entries[file] = entry

		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			entry.load(file)
		}()
	}
}
