// Package sqliteconfig owns how hscontrol/db reaches SQLite. It registers
// mattn/go-sqlite3 under [DriverName] for callers that open by name, and
// [Open] builds a pool whose every connection gets the pragmas from
// [Config] applied through a [driver.Connector], so the settings survive
// pool reconnects. Connection hardening (defensive mode and strict double
// quotes) is compiled into the library by the flags in sqlite.cflags;
// [ProbeHardening] reports whether a binary carries them.
package sqliteconfig

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/mattn/go-sqlite3"
)

// DriverName is the database/sql driver name this package registers for
// mattn/go-sqlite3. It carries no pragmas; production pools come from
// [Open], the name is for read-only consumers such as tailsql and the jet
// generator.
const DriverName = "sqlite"

//nolint:gochecknoinits // database/sql drivers register at init; mattn does the same for "sqlite3"
func init() {
	sql.Register(DriverName, &sqlite3.SQLiteDriver{})
}

// Errors returned by config validation.
var (
	ErrPathEmpty           = errors.New("path cannot be empty")
	ErrBusyTimeoutNegative = errors.New("busy_timeout must be >= 0")
	ErrInvalidJournalMode  = errors.New("invalid journal_mode")
	ErrInvalidAutoVacuum   = errors.New("invalid auto_vacuum")
	ErrWALAutocheckpoint   = errors.New("wal_autocheckpoint must be >= -1")
	ErrInvalidSynchronous  = errors.New("invalid synchronous")
	ErrInvalidTxLock       = errors.New("invalid txlock")
	ErrCacheSizeNegative   = errors.New("cache_size must be >= 0")
)

const (
	// DefaultBusyTimeout is the default busy timeout in milliseconds.
	DefaultBusyTimeout = 10000
	// DefaultWALAutocheckpoint is the default number of WAL pages before
	// an automatic checkpoint.
	DefaultWALAutocheckpoint = 1000
	// DefaultCacheSize is the default page cache size in KiB (64 MiB).
	// SQLite's own default of 2 MiB is sized for tiny embedded uses; the
	// whole working set of a control server fits in this cache, so reads
	// stop touching the file at all once warm.
	DefaultCacheSize = 64 * 1024
	// DefaultMmapSize maps up to 256 MiB of the file. A database smaller
	// than that is mapped whole; the mapping costs no memory beyond the
	// page cache the OS keeps anyway.
	DefaultMmapSize int64 = 256 << 20
)

// JournalMode represents SQLite journal_mode pragma values.
// Journal modes control how SQLite handles write transactions and crash recovery.
//
// Performance vs Durability Tradeoffs:
//
// WAL (Write-Ahead Logging) - Recommended for production:
//   - Best performance for concurrent reads/writes
//   - Readers don't block writers, writers don't block readers
//   - Excellent crash recovery with minimal data loss risk
//   - Uses additional .wal and .shm files
//   - Default choice for Slopscale production deployments
//
// DELETE - Traditional rollback journal:
//   - Good performance for single-threaded access
//   - Readers block writers and vice versa
//   - Reliable crash recovery but with exclusive locking
//   - Creates temporary journal files during transactions
//   - Suitable for low-concurrency scenarios
//
// TRUNCATE - Similar to DELETE but faster cleanup:
//   - Slightly better performance than DELETE
//   - Same concurrency limitations as DELETE
//   - Faster transaction commit by truncating instead of deleting journal
//
// PERSIST - Journal file remains between transactions:
//   - Avoids file creation/deletion overhead
//   - Same concurrency limitations as DELETE
//   - Good for frequent small transactions
//
// MEMORY - Journal kept in memory:
//   - Fastest performance but NO crash recovery
//   - Data loss risk on power failure or crash
//   - Only suitable for temporary or non-critical data
//
// OFF - No journaling:
//   - Maximum performance but NO transaction safety
//   - High risk of database corruption on crash
//   - Should only be used for read-only or disposable databases
type JournalMode string

const (
	// JournalModeWAL enables Write-Ahead Logging (RECOMMENDED for production).
	// Best concurrent performance + crash recovery. Uses additional .wal/.shm files.
	JournalModeWAL JournalMode = "WAL"

	// JournalModeDelete uses traditional rollback journaling.
	// Good single-threaded performance, readers block writers. Creates temp journal files.
	JournalModeDelete JournalMode = "DELETE"

	// JournalModeTruncate is like DELETE but with faster cleanup.
	// Slightly better performance than DELETE, same safety with exclusive locking.
	JournalModeTruncate JournalMode = "TRUNCATE"

	// JournalModePersist keeps journal file between transactions.
	// Good for frequent transactions, avoids file creation/deletion overhead.
	JournalModePersist JournalMode = "PERSIST"

	// JournalModeMemory keeps journal in memory (DANGEROUS).
	// Fastest performance but NO crash recovery - data loss on power failure.
	JournalModeMemory JournalMode = "MEMORY"

	// JournalModeOff disables journaling entirely (EXTREMELY DANGEROUS).
	// Maximum performance but high corruption risk. Only for disposable databases.
	JournalModeOff JournalMode = "OFF"
)

// validJournalModes lists the accepted JournalMode values.
var validJournalModes = []JournalMode{
	JournalModeWAL, JournalModeDelete, JournalModeTruncate,
	JournalModePersist, JournalModeMemory, JournalModeOff,
}

// IsValid returns true if the JournalMode is valid.
func (j JournalMode) IsValid() bool {
	return slices.Contains(validJournalModes, j)
}

// AutoVacuum represents SQLite auto_vacuum pragma values.
// Auto-vacuum controls how SQLite reclaims space from deleted data.
//
// Performance vs Storage Tradeoffs:
//
// INCREMENTAL - Recommended for production:
//   - Reclaims space gradually during normal operations
//   - Minimal performance impact on writes
//   - Database size shrinks automatically over time
//   - Can manually trigger with PRAGMA incremental_vacuum
//   - Good balance of space efficiency and performance
//
// FULL - Automatic space reclamation:
//   - Immediately reclaims space on every DELETE/DROP
//   - Higher write overhead due to page reorganization
//   - Keeps database file size minimal
//   - Can cause significant slowdowns on large deletions
//   - Best for applications with frequent deletes and limited storage
//
// NONE - No automatic space reclamation:
//   - Fastest write performance (no vacuum overhead)
//   - Database file only grows, never shrinks
//   - Deleted space is reused but file size remains large
//   - Requires manual VACUUM to reclaim space
//   - Best for write-heavy workloads where storage isn't constrained
type AutoVacuum string

const (
	// AutoVacuumNone disables automatic space reclamation.
	// Fastest writes, file only grows. Requires manual VACUUM to reclaim space.
	AutoVacuumNone AutoVacuum = "NONE"

	// AutoVacuumFull immediately reclaims space on every DELETE/DROP.
	// Minimal file size but slower writes. Can impact performance on large deletions.
	AutoVacuumFull AutoVacuum = "FULL"

	// AutoVacuumIncremental reclaims space gradually (RECOMMENDED for production).
	// Good balance: minimal write impact, automatic space management over time.
	AutoVacuumIncremental AutoVacuum = "INCREMENTAL"
)

// validAutoVacuums lists the accepted AutoVacuum values.
var validAutoVacuums = []AutoVacuum{
	AutoVacuumNone, AutoVacuumFull, AutoVacuumIncremental,
}

// IsValid returns true if the AutoVacuum is valid.
func (a AutoVacuum) IsValid() bool {
	return slices.Contains(validAutoVacuums, a)
}

// Synchronous represents SQLite synchronous pragma values.
// Synchronous mode controls how aggressively SQLite flushes data to disk.
//
// Performance vs Durability Tradeoffs:
//
// NORMAL - Recommended for production:
//   - Good balance of performance and safety
//   - Syncs at critical moments (transaction commits in WAL mode)
//   - Very low risk of corruption, minimal performance impact
//   - Safe with WAL mode even with power loss
//   - Default choice for most production applications
//
// FULL - Maximum durability:
//   - Syncs to disk after every write operation
//   - Highest data safety, virtually no corruption risk
//   - Significant performance penalty (up to 50% slower)
//   - Recommended for critical data where corruption is unacceptable
//
// EXTRA - Paranoid mode:
//   - Even more aggressive syncing than FULL
//   - Maximum possible data safety
//   - Severe performance impact
//   - Only for extremely critical scenarios
//
// OFF - Maximum performance, minimum safety:
//   - No syncing, relies on OS to flush data
//   - Fastest possible performance
//   - High risk of corruption on power failure or crash
//   - Only suitable for non-critical or easily recreatable data
type Synchronous string

const (
	// SynchronousOff disables syncing (DANGEROUS).
	// Fastest performance but high corruption risk on power failure. Avoid in production.
	SynchronousOff Synchronous = "OFF"

	// SynchronousNormal provides balanced performance and safety (RECOMMENDED).
	// Good performance with low corruption risk. Safe with WAL mode on power loss.
	SynchronousNormal Synchronous = "NORMAL"

	// SynchronousFull provides maximum durability with performance cost.
	// Syncs after every write. Up to 50% slower but virtually no corruption risk.
	SynchronousFull Synchronous = "FULL"

	// SynchronousExtra provides paranoid-level data safety (EXTREME).
	// Maximum safety with severe performance impact. Rarely needed in practice.
	SynchronousExtra Synchronous = "EXTRA"
)

// validSynchronous lists the accepted Synchronous values.
var validSynchronous = []Synchronous{
	SynchronousOff, SynchronousNormal, SynchronousFull, SynchronousExtra,
}

// IsValid returns true if the Synchronous is valid.
func (s Synchronous) IsValid() bool {
	return slices.Contains(validSynchronous, s)
}

// TxLock represents SQLite transaction lock mode.
// Transaction lock mode determines when write locks are acquired during transactions.
//
// Lock Acquisition Behavior:
//
// DEFERRED - SQLite default, acquire lock lazily:
//   - Transaction starts without any lock
//   - First read acquires SHARED lock
//   - First write attempts to upgrade to RESERVED lock
//   - If another transaction holds RESERVED: SQLITE_BUSY (potential deadlock)
//   - Can cause deadlocks when multiple connections attempt concurrent writes
//
// IMMEDIATE - Recommended for write-heavy workloads:
//   - Transaction immediately acquires RESERVED lock at BEGIN
//   - If lock unavailable, waits up to busy_timeout before failing
//   - Other writers queue orderly instead of deadlocking
//   - Prevents the upgrade-lock deadlock scenario
//   - Slight overhead for read-only transactions that don't need locks
//
// EXCLUSIVE - Maximum isolation:
//   - Transaction immediately acquires EXCLUSIVE lock at BEGIN
//   - No other connections can read or write
//   - Highest isolation but lowest concurrency
//   - Rarely needed in practice
type TxLock string

const (
	// TxLockDeferred acquires locks lazily (SQLite default).
	// Risk of SQLITE_BUSY deadlocks with concurrent writers. Use for read-heavy workloads.
	TxLockDeferred TxLock = "deferred"

	// TxLockImmediate acquires write lock immediately (RECOMMENDED for production).
	// Prevents deadlocks by acquiring RESERVED lock at transaction start.
	// Writers queue orderly, respecting busy_timeout.
	TxLockImmediate TxLock = "immediate"

	// TxLockExclusive acquires exclusive lock immediately.
	// Maximum isolation, no concurrent reads or writes. Rarely needed.
	TxLockExclusive TxLock = "exclusive"
)

// validTxLocks lists the accepted TxLock values; the empty string is valid
// and selects the driver default.
var validTxLocks = []TxLock{
	TxLockDeferred, TxLockImmediate, TxLockExclusive, "",
}

// IsValid returns true if the TxLock is valid.
func (t TxLock) IsValid() bool {
	return slices.Contains(validTxLocks, t)
}

// Config holds SQLite database configuration with type-safe enums.
// This configuration balances performance, durability, and operational requirements
// for Slopscale's SQLite database usage patterns.
type Config struct {
	Path              string      // file path or ":memory:"
	BusyTimeout       int         // milliseconds (0 = default/disabled)
	JournalMode       JournalMode // journal mode (affects concurrency and crash recovery)
	AutoVacuum        AutoVacuum  // auto vacuum mode (affects storage efficiency)
	WALAutocheckpoint int         // pages (-1 = default/not set, 0 = disabled, >0 = enabled)
	Synchronous       Synchronous // synchronous mode (affects durability vs performance)
	ForeignKeys       bool        // enable foreign key constraints (data integrity)
	TxLock            TxLock      // transaction lock mode (affects write concurrency)
	// CacheSize is the page cache size per connection in KiB; 0 leaves
	// SQLite's default (2 MiB).
	CacheSize int
	// MmapSize is the size in bytes of the memory map SQLite reads the
	// file through; 0 leaves it off. Mapped pages are served from the OS
	// page cache without a copy into the connection's cache.
	MmapSize int64
	// TempStoreMemory keeps temporary tables and sort files in memory
	// rather than on disk.
	TempStoreMemory bool
}

// Default returns the production configuration optimized for Slopscale's usage patterns.
// This configuration prioritizes:
//   - Concurrent access (WAL mode for multiple readers/writers)
//   - Data durability with good performance (NORMAL synchronous)
//   - Automatic space management (INCREMENTAL auto-vacuum)
//   - Data integrity (foreign key constraints enabled)
//   - Safe concurrent writes (IMMEDIATE transaction lock)
//   - Reasonable timeout for busy database scenarios (10s)
func Default(path string) *Config {
	return &Config{
		Path:              path,
		BusyTimeout:       DefaultBusyTimeout,
		JournalMode:       JournalModeWAL,
		AutoVacuum:        AutoVacuumIncremental,
		WALAutocheckpoint: DefaultWALAutocheckpoint,
		Synchronous:       SynchronousNormal,
		ForeignKeys:       true,
		TxLock:            TxLockImmediate,
		CacheSize:         DefaultCacheSize,
		MmapSize:          DefaultMmapSize,
		TempStoreMemory:   true,
	}
}

// Memory returns a configuration for in-memory databases.
func Memory() *Config {
	return &Config{
		Path:              ":memory:",
		WALAutocheckpoint: -1, // not set, use driver default
		ForeignKeys:       true,
	}
}

// Validate checks if all configuration values are valid.
func (c *Config) Validate() error {
	if c.Path == "" {
		return ErrPathEmpty
	}

	if c.BusyTimeout < 0 {
		return fmt.Errorf("%w, got %d", ErrBusyTimeoutNegative, c.BusyTimeout)
	}

	if c.JournalMode != "" && !c.JournalMode.IsValid() {
		return fmt.Errorf("%w: %s", ErrInvalidJournalMode, c.JournalMode)
	}

	if c.AutoVacuum != "" && !c.AutoVacuum.IsValid() {
		return fmt.Errorf("%w: %s", ErrInvalidAutoVacuum, c.AutoVacuum)
	}

	if c.WALAutocheckpoint < -1 {
		return fmt.Errorf("%w, got %d", ErrWALAutocheckpoint, c.WALAutocheckpoint)
	}

	if c.Synchronous != "" && !c.Synchronous.IsValid() {
		return fmt.Errorf("%w: %s", ErrInvalidSynchronous, c.Synchronous)
	}

	if c.CacheSize < 0 {
		return fmt.Errorf("%w: %d", ErrCacheSizeNegative, c.CacheSize)
	}

	if c.TxLock != "" && !c.TxLock.IsValid() {
		return fmt.Errorf("%w: %s", ErrInvalidTxLock, c.TxLock)
	}

	return nil
}

// DSN returns the mattn/go-sqlite3 data source name for the configuration:
// the path plus the transaction lock mode, which is the one setting the
// driver must know at BEGIN time and cannot be a pragma. Everything else is
// applied per connection from [Config.Pragmas].
func (c *Config) DSN() string {
	if c.TxLock == "" {
		return c.Path
	}

	return c.Path + "?_txlock=" + string(c.TxLock)
}

// Pragmas returns the PRAGMA statements that realise the configuration,
// in the order they must run: busy_timeout first so the ones after it wait
// on a locked file instead of failing; auto_vacuum before journal_mode,
// because switching to WAL writes the database header of a new file and
// after that auto_vacuum can only change through VACUUM; foreign_keys
// last.
func (c *Config) Pragmas() []string {
	var pragmas []string

	if c.BusyTimeout > 0 {
		pragmas = append(pragmas, "PRAGMA busy_timeout = "+strconv.Itoa(c.BusyTimeout))
	}

	if c.AutoVacuum != "" {
		pragmas = append(pragmas, "PRAGMA auto_vacuum = "+string(c.AutoVacuum))
	}

	if c.JournalMode != "" {
		pragmas = append(pragmas, "PRAGMA journal_mode = "+string(c.JournalMode))
	}

	if c.WALAutocheckpoint >= 0 {
		pragmas = append(pragmas, "PRAGMA wal_autocheckpoint = "+strconv.Itoa(c.WALAutocheckpoint))
	}

	if c.Synchronous != "" {
		pragmas = append(pragmas, "PRAGMA synchronous = "+string(c.Synchronous))
	}

	// A negative cache_size is a size in KiB rather than a page count.
	if c.CacheSize > 0 {
		pragmas = append(pragmas, "PRAGMA cache_size = -"+strconv.Itoa(c.CacheSize))
	}

	if c.MmapSize > 0 {
		pragmas = append(pragmas, "PRAGMA mmap_size = "+strconv.FormatInt(c.MmapSize, 10))
	}

	if c.TempStoreMemory {
		pragmas = append(pragmas, "PRAGMA temp_store = MEMORY")
	}

	if c.ForeignKeys {
		pragmas = append(pragmas, "PRAGMA foreign_keys = ON")
	}

	return pragmas
}
