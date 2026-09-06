// Package sqliteconfig provides type-safe configuration for SQLite databases
// with proper enum validation and URL generation for the modernc.org/sqlite
// driver, which hscontrol/db opens through database/sql. Besides pragmas it
// carries two connection-level hardening switches: defensive mode, which
// refuses SQL that can deliberately corrupt the file (writable_schema and
// friends), and strict double quotes, which stops SQLite from silently
// treating an unknown double-quoted identifier as a string literal.
package sqliteconfig

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// DriverName is the database/sql driver name modernc.org/sqlite registers.
const DriverName = "sqlite"

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
//   - Default choice for Headscale production deployments
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
// for Headscale's SQLite database usage patterns.
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
	// Defensive enables SQLITE_DBCONFIG_DEFENSIVE (_defensive=1): PRAGMA
	// writable_schema, journal_mode=OFF, schema_version writes and direct
	// shadow-table writes are refused, closing the SQL-level corruption vectors.
	Defensive bool
	// StrictDoubleQuotes disables the double-quoted string literal
	// misfeature (_dqs=0), so a mistyped "identifier" is an error instead of
	// a string.
	StrictDoubleQuotes bool
}

// Default returns the production configuration optimized for Headscale's usage patterns.
// This configuration prioritizes:
//   - Concurrent access (WAL mode for multiple readers/writers)
//   - Data durability with good performance (NORMAL synchronous)
//   - Automatic space management (INCREMENTAL auto-vacuum)
//   - Data integrity (foreign key constraints enabled)
//   - Safe concurrent writes (IMMEDIATE transaction lock)
//   - Reasonable timeout for busy database scenarios (10s)
//   - Corruption-resistant connections (defensive mode, strict double quotes)
func Default(path string) *Config {
	return &Config{
		Path:               path,
		BusyTimeout:        DefaultBusyTimeout,
		JournalMode:        JournalModeWAL,
		AutoVacuum:         AutoVacuumIncremental,
		WALAutocheckpoint:  DefaultWALAutocheckpoint,
		Synchronous:        SynchronousNormal,
		ForeignKeys:        true,
		TxLock:             TxLockImmediate,
		CacheSize:          DefaultCacheSize,
		Defensive:          true,
		StrictDoubleQuotes: true,
	}
}

// Memory returns a configuration for in-memory databases.
func Memory() *Config {
	return &Config{
		Path:               ":memory:",
		WALAutocheckpoint:  -1, // not set, use driver default
		ForeignKeys:        true,
		Defensive:          true,
		StrictDoubleQuotes: true,
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

// ToURL builds a properly encoded SQLite connection string using _pragma parameters
// compatible with modernc.org/sqlite driver.
func (c *Config) ToURL() (string, error) {
	err := c.Validate()
	if err != nil {
		return "", fmt.Errorf("invalid config: %w", err)
	}

	// Handle different database types
	var baseURL string
	if c.Path == ":memory:" {
		baseURL = ":memory:"
	} else {
		baseURL = "file:" + c.Path
	}

	// Build query parameters
	var queryParts []string

	// Connection parameters come first, then pragmas. _txlock leads because
	// it changes how every transaction below is opened.
	if c.TxLock != "" {
		queryParts = append(queryParts, "_txlock="+string(c.TxLock))
	}

	if c.Defensive {
		queryParts = append(queryParts, "_defensive=1")
	}

	if c.StrictDoubleQuotes {
		queryParts = append(queryParts, "_dqs=0")
	}

	// Add pragma parameters only if they're set (non-zero/non-empty)
	if c.BusyTimeout > 0 {
		queryParts = append(queryParts, fmt.Sprintf("_pragma=busy_timeout=%d", c.BusyTimeout))
	}

	if c.JournalMode != "" {
		queryParts = append(queryParts, fmt.Sprintf("_pragma=journal_mode=%s", c.JournalMode))
	}

	if c.AutoVacuum != "" {
		queryParts = append(queryParts, fmt.Sprintf("_pragma=auto_vacuum=%s", c.AutoVacuum))
	}

	if c.WALAutocheckpoint >= 0 {
		queryParts = append(queryParts, fmt.Sprintf("_pragma=wal_autocheckpoint=%d", c.WALAutocheckpoint))
	}

	if c.Synchronous != "" {
		queryParts = append(queryParts, fmt.Sprintf("_pragma=synchronous=%s", c.Synchronous))
	}

	// A negative cache_size is a size in KiB rather than a page count.
	if c.CacheSize > 0 {
		queryParts = append(queryParts, fmt.Sprintf("_pragma=cache_size=-%d", c.CacheSize))
	}

	if c.ForeignKeys {
		queryParts = append(queryParts, "_pragma=foreign_keys=ON")
	}

	if len(queryParts) > 0 {
		baseURL += "?" + strings.Join(queryParts, "&")
	}

	return baseURL, nil
}
