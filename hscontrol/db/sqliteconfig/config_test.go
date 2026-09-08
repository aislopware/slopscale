package sqliteconfig

import (
	"slices"
	"testing"
)

func TestJournalMode(t *testing.T) {
	tests := []struct {
		mode  JournalMode
		valid bool
	}{
		{JournalModeWAL, true},
		{JournalModeDelete, true},
		{JournalModeTruncate, true},
		{JournalModePersist, true},
		{JournalModeMemory, true},
		{JournalModeOff, true},
		{JournalMode("INVALID"), false},
		{JournalMode(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.valid {
				t.Errorf("JournalMode(%q).IsValid() = %v, want %v", tt.mode, got, tt.valid)
			}
		})
	}
}

func TestAutoVacuum(t *testing.T) {
	tests := []struct {
		mode  AutoVacuum
		valid bool
	}{
		{AutoVacuumNone, true},
		{AutoVacuumFull, true},
		{AutoVacuumIncremental, true},
		{AutoVacuum("INVALID"), false},
		{AutoVacuum(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.valid {
				t.Errorf("AutoVacuum(%q).IsValid() = %v, want %v", tt.mode, got, tt.valid)
			}
		})
	}
}

func TestSynchronous(t *testing.T) {
	tests := []struct {
		mode  Synchronous
		valid bool
	}{
		{SynchronousOff, true},
		{SynchronousNormal, true},
		{SynchronousFull, true},
		{SynchronousExtra, true},
		{Synchronous("INVALID"), false},
		{Synchronous(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.valid {
				t.Errorf("Synchronous(%q).IsValid() = %v, want %v", tt.mode, got, tt.valid)
			}
		})
	}
}

func TestTxLock(t *testing.T) {
	tests := []struct {
		mode  TxLock
		valid bool
	}{
		{TxLockDeferred, true},
		{TxLockImmediate, true},
		{TxLockExclusive, true},
		{TxLock(""), true},           // empty is valid (uses driver default)
		{TxLock("IMMEDIATE"), false}, // uppercase is invalid
		{TxLock("INVALID"), false},
	}

	for _, tt := range tests {
		name := string(tt.mode)
		if name == "" {
			name = "empty"
		}

		t.Run(name, func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.valid {
				t.Errorf("TxLock(%q).IsValid() = %v, want %v", tt.mode, got, tt.valid)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:   "valid default config",
			config: Default("/path/to/db.sqlite"),
		},
		{
			name: "empty path",
			config: &Config{
				Path: "",
			},
			wantErr: true,
		},
		{
			name: "negative busy timeout",
			config: &Config{
				Path:        "/path/to/db.sqlite",
				BusyTimeout: -1,
			},
			wantErr: true,
		},
		{
			name: "invalid journal mode",
			config: &Config{
				Path:        "/path/to/db.sqlite",
				JournalMode: JournalMode("INVALID"),
			},
			wantErr: true,
		},
		{
			name: "invalid txlock",
			config: &Config{
				Path:   "/path/to/db.sqlite",
				TxLock: TxLock("INVALID"),
			},
			wantErr: true,
		},
		{
			name: "valid txlock immediate",
			config: &Config{
				Path:   "/path/to/db.sqlite",
				TxLock: TxLockImmediate,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigDSN(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   string
	}{
		{
			name:   "default config carries txlock immediate",
			config: Default("/path/to/db.sqlite"),
			want:   "/path/to/db.sqlite?_txlock=immediate",
		},
		{
			name:   "memory config",
			config: Memory(),
			want:   ":memory:",
		},
		{
			name:   "txlock deferred",
			config: &Config{Path: "/test.db", TxLock: TxLockDeferred, WALAutocheckpoint: -1},
			want:   "/test.db?_txlock=deferred",
		},
		{
			name:   "txlock exclusive",
			config: &Config{Path: "/test.db", TxLock: TxLockExclusive, WALAutocheckpoint: -1},
			want:   "/test.db?_txlock=exclusive",
		},
		{
			name:   "empty txlock omitted",
			config: &Config{Path: "/test.db", WALAutocheckpoint: -1},
			want:   "/test.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.DSN(); got != tt.want {
				t.Errorf("Config.DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigPragmas(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   []string
	}{
		{
			name:   "default config",
			config: Default("/path/to/db.sqlite"),
			want: []string{
				"PRAGMA busy_timeout = 10000",
				"PRAGMA auto_vacuum = INCREMENTAL",
				"PRAGMA journal_mode = WAL",
				"PRAGMA wal_autocheckpoint = 1000",
				"PRAGMA synchronous = NORMAL",
				"PRAGMA cache_size = -65536",
				"PRAGMA mmap_size = 268435456",
				"PRAGMA temp_store = MEMORY",
				"PRAGMA foreign_keys = ON",
			},
		},
		{
			name:   "memory config",
			config: Memory(),
			want:   []string{"PRAGMA foreign_keys = ON"},
		},
		{
			name:   "minimal config",
			config: &Config{Path: "/simple/db.sqlite", WALAutocheckpoint: -1},
			want:   nil,
		},
		{
			name: "custom config",
			config: &Config{
				Path:              "/custom/db.sqlite",
				BusyTimeout:       5000,
				JournalMode:       JournalModeDelete,
				WALAutocheckpoint: -1,
				Synchronous:       SynchronousFull,
				ForeignKeys:       true,
			},
			want: []string{
				"PRAGMA busy_timeout = 5000",
				"PRAGMA journal_mode = DELETE",
				"PRAGMA synchronous = FULL",
				"PRAGMA foreign_keys = ON",
			},
		},
		{
			name:   "wal autocheckpoint zero is emitted",
			config: &Config{Path: "/test.db", WALAutocheckpoint: 0},
			want:   []string{"PRAGMA wal_autocheckpoint = 0"},
		},
		{
			name: "all options",
			config: &Config{
				Path:              "/full.db",
				BusyTimeout:       15000,
				JournalMode:       JournalModeWAL,
				AutoVacuum:        AutoVacuumFull,
				WALAutocheckpoint: 1000,
				Synchronous:       SynchronousExtra,
				ForeignKeys:       true,
				CacheSize:         2048,
			},
			want: []string{
				"PRAGMA busy_timeout = 15000",
				"PRAGMA auto_vacuum = FULL",
				"PRAGMA journal_mode = WAL",
				"PRAGMA wal_autocheckpoint = 1000",
				"PRAGMA synchronous = EXTRA",
				"PRAGMA cache_size = -2048",
				"PRAGMA foreign_keys = ON",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.Pragmas()
			if !slices.Equal(got, tt.want) {
				t.Errorf("Config.Pragmas() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewConnectorInvalid(t *testing.T) {
	config := &Config{
		Path:        "",
		BusyTimeout: -1,
	}

	_, err := NewConnector(config)
	if err == nil {
		t.Error("NewConnector() with invalid config should return error")
	}
}

func TestDefaultConfigHasTxLockImmediate(t *testing.T) {
	config := Default("/test.db")
	if config.TxLock != TxLockImmediate {
		t.Errorf("Default().TxLock = %q, want %q", config.TxLock, TxLockImmediate)
	}
}
