package types

import (
	"errors"
	"strconv"
	"time"
)

// SSHRecorderTag is the tag the embedded session recorder registers
// with, so policy aliases and the tailnet default can name it.
const SSHRecorderTag = "tag:headscale-recorder"

// SSHRecorderHostname is the embedded recorder's node name.
const SSHRecorderHostname = "headscale-recorder"

// SSHRecorderPort is the port a session recorder listens on; the client
// speaks plain HTTP to it over the tailnet.
const SSHRecorderPort = 80

// ErrSSHRecorderInvalid is returned for a default recorder that is not a
// tag, a host or an address.
var ErrSSHRecorderInvalid = errors.New("invalid SSH recorder")

// SSHRecordingConfig is the embedded session recorder; see
// docs/ref/ssh-recording.md.
type SSHRecordingConfig struct {
	// Enabled runs a recorder node inside the server and makes it a
	// default recorder.
	Enabled bool
	// Dir is where recordings are written, one asciinema file each.
	Dir string
	// StateDir holds the recorder node's keys.
	StateDir string
	// Retention deletes recordings older than this; zero keeps them.
	Retention time.Duration
}

// SSHRecordingID identifies a recording in the ssh_recordings table.
type SSHRecordingID uint64

// String renders the ID in base 10.
func (id SSHRecordingID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// SSHRecording is one recorded SSH session: who connected from where to
// what, and the asciinema file that holds the terminal output.
type SSHRecording struct {
	ID        SSHRecordingID
	StartedAt time.Time
	// EndedAt is when the upload finished; nil while it runs or when it
	// broke off.
	EndedAt *time.Time
	// SrcNode and SrcNodeID name the connecting node, as the client
	// reported them; SrcUser is its user's login name for a user-owned
	// node, empty for a tagged one.
	SrcNode   string
	SrcNodeID string
	SrcUser   string
	// DstNodeID is the node the session ran on, known from the address
	// the upload came from; zero when it could not be told.
	DstNodeID NodeID
	DstNode   string
	// SSHUser is the name the client asked for, LocalUser the account
	// the session got.
	SSHUser   string
	LocalUser string
	// Command is what ran instead of a shell; empty for a shell.
	Command string
	// Size is the file's size in bytes.
	Size int64
	// Path is the file, relative to the recordings directory.
	Path string
	// Complete is whether the upload ended cleanly.
	Complete bool
}

// ErrSSHRecordingNotFound is returned for an unknown recording.
var ErrSSHRecordingNotFound = errors.New("SSH recording not found")
