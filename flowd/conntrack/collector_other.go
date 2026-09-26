//go:build !linux

package conntrack

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// ErrUnsupported is returned where connection tracking cannot be read.
var ErrUnsupported = errors.New("connection tracking needs Linux")

// Collector reads the kernel's connection tracking table; it only works
// on Linux.
type Collector struct {
	Interval time.Duration
	Sink     func(Delta)
	Log      *slog.Logger
}

// Run fails: there is no connection tracking table to read.
func (*Collector) Run(context.Context) error {
	return ErrUnsupported
}
