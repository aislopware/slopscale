package dns

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/fsnotify/fsnotify"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/util/set"
)

// ErrPathIsDirectory is returned when a directory path is provided where a file is expected.
var ErrPathIsDirectory = errors.New("path is a directory, only file is supported")

// extraRecordsSettle is how long the watched file must be quiet before it
// is read again. An editor or a script writes the file in several steps
// (truncate, write, rename, chmod), each an event of its own, and reading
// after the first one sees a partial file that fails to parse
// (juanfont/headscale#2753).
const extraRecordsSettle = 100 * time.Millisecond

type ExtraRecordsMan struct {
	mu      sync.RWMutex
	records set.Set[tailcfg.DNSRecord]
	watcher *fsnotify.Watcher
	path    string

	updateCh chan []tailcfg.DNSRecord
	closeCh  chan struct{}
	hash     [32]byte

	// settle is armed by a write event and fires once the file has been
	// quiet for [extraRecordsSettle]; nil until the first event.
	settle *time.Timer
}

// NewExtraRecordsManager creates a new [ExtraRecordsMan] and starts watching the file at the given path.
func NewExtraRecordsManager(path string) (*ExtraRecordsMan, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating watcher: %w", err)
	}

	closeWatcher := func() {
		_ = watcher.Close()
	}

	fi, err := os.Stat(path)
	if err != nil {
		closeWatcher()
		return nil, fmt.Errorf("getting file info: %w", err)
	}

	if fi.IsDir() {
		closeWatcher()
		return nil, fmt.Errorf("%w: %s", ErrPathIsDirectory, path)
	}

	records, hash, err := readExtraRecordsFromPath(path)
	if err != nil {
		closeWatcher()
		return nil, fmt.Errorf("reading extra records from path: %w", err)
	}

	er := &ExtraRecordsMan{
		watcher:  watcher,
		path:     path,
		records:  set.SetOf(records),
		hash:     hash,
		closeCh:  make(chan struct{}),
		updateCh: make(chan []tailcfg.DNSRecord),
	}

	err = watcher.Add(path)
	if err != nil {
		closeWatcher()

		return nil, fmt.Errorf("adding path to watcher: %w", err)
	}

	log.Trace().Caller().Strs("watching", watcher.WatchList()).Msg("started filewatcher")

	return er, nil
}

func (e *ExtraRecordsMan) Records() []tailcfg.DNSRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.records.Slice()
}

func (e *ExtraRecordsMan) Run() {
	// A stopped timer whose channel never fires until the first event
	// arms it, so the select below needs no nil check.
	e.settle = time.NewTimer(time.Hour)
	e.settle.Stop()

	defer e.settle.Stop()

	for {
		select {
		case <-e.closeCh:
			return
		case <-e.settle.C:
			e.updateRecords()
		case event, ok := <-e.watcher.Events:
			if !ok {
				log.Error().Caller().Msgf("file watcher event channel closing")
				return
			}

			if !e.handleWatchEvent(event) {
				return
			}

		case err, ok := <-e.watcher.Errors:
			if !ok {
				log.Error().Caller().Msgf("file watcher error channel closing")
				return
			}

			log.Error().Caller().Err(err).Msgf("extra records filewatcher returned error: %q", err)
		}
	}
}

func (e *ExtraRecordsMan) Close() {
	e.watcher.Close()
	close(e.closeCh)
}

func (e *ExtraRecordsMan) UpdateCh() <-chan []tailcfg.DNSRecord {
	return e.updateCh
}

// handleWatchEvent processes a single fsnotify event for the watched file.
// It reports whether Run should keep looping; false means the caller must
// stop the goroutine.
//
// An event may carry several operations at once (kqueue reports
// WRITE|CHMOD for a truncating rewrite), so each is tested as a bit, not
// as the whole value.
func (e *ExtraRecordsMan) handleWatchEvent(event fsnotify.Event) bool {
	// If a file is removed or renamed, fsnotify will lose track of it
	// and not watch it. We will therefore attempt to re-add it with a backoff.
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		return e.handleWatchRemoveOrRename()
	}

	if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) && !event.Has(fsnotify.Chmod) {
		return true
	}

	log.Trace().
		Caller().
		Str("path", event.Name).
		Str("op", event.Op.String()).
		Msg("extra records received filewatch event")

	if event.Name != e.path {
		return true
	}

	// Read once the writer has gone quiet, not on every step of the
	// write.
	e.settle.Reset(extraRecordsSettle)

	return true
}

// handleWatchRemoveOrRename waits for the watched file to reappear after a
// remove/rename event and re-adds it to the watcher. It reports whether Run
// should keep looping; false means the caller must stop the goroutine.
func (e *ExtraRecordsMan) handleWatchRemoveOrRename() bool {
	err := e.waitUntilPathExists()
	if err != nil {
		select {
		case <-e.closeCh:
			return false
		default:
		}

		log.Error().Caller().Err(err).Msgf("extra records filewatcher retrying to find file after delete")

		addErr := e.watcher.Add(filepath.Dir(e.path))
		if addErr != nil {
			log.Error().
				Caller().
				Err(addErr).
				Msgf("extra records filewatcher watching parent after delete failed")
		}

		return true
	}

	err = e.watcher.Add(e.path)
	if err != nil {
		log.Error().
			Caller().
			Err(err).
			Msgf("extra records filewatcher re-adding file after delete failed, giving up.")

		return false
	}

	log.Trace().Caller().Str("path", e.path).Msg("extra records file re-added after delete")
	e.updateRecords()

	return true
}

func (e *ExtraRecordsMan) waitUntilPathExists() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-e.closeCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		_, statErr := os.Stat(e.path)
		if statErr != nil {
			return struct{}{}, fmt.Errorf("stat extra records file %q: %w", e.path, statErr)
		}

		return struct{}{}, nil
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
	if err != nil {
		return fmt.Errorf("waiting for extra records file %q: %w", e.path, err)
	}

	return nil
}

func (e *ExtraRecordsMan) updateRecords() {
	records, newHash, err := readExtraRecordsFromPath(e.path)
	if err != nil {
		log.Error().Caller().Err(err).Msgf("reading extra records from path: %s", e.path)
		return
	}

	// If there are no records, ignore the update.
	if records == nil {
		return
	}

	e.mu.Lock()

	// If there has not been any change, ignore the update.
	if newHash == e.hash {
		e.mu.Unlock()

		return
	}

	oldCount := e.records.Len()

	e.records = set.SetOf(records)
	e.hash = newHash
	toSend := e.records.Slice()

	log.Trace().
		Caller().
		Interface("records", e.records).
		Msgf("extra records updated from path, count old: %d, new: %d", oldCount, e.records.Len())

	// Release the lock before the (potentially blocking) send so a slow or
	// absent consumer cannot stall Records() readers, and abort the send on
	// shutdown instead of leaking this goroutine on the closed-down channel.
	e.mu.Unlock()

	select {
	case e.updateCh <- toSend:
	case <-e.closeCh:
	}
}

// readExtraRecordsFromPath reads a JSON file of [tailcfg.DNSRecord]
// and returns the records and the hash of the file.
func readExtraRecordsFromPath(path string) ([]tailcfg.DNSRecord, [32]byte, error) {
	var zero [32]byte

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, zero, fmt.Errorf("reading path: %s, err: %w", path, err)
	}

	// If the read was triggered too fast, and the file is not complete, ignore the update
	// if the file is empty. A consecutive update will be triggered when the file is complete.
	if len(b) == 0 {
		return nil, zero, nil
	}

	var records []tailcfg.DNSRecord

	err = json.Unmarshal(b, &records)
	if err != nil {
		return nil, zero, fmt.Errorf("unmarshalling records, content: %q: %w", string(b), err)
	}

	hash := sha256.Sum256(b)

	return types.NormalizeExtraRecords(records), hash, nil
}
