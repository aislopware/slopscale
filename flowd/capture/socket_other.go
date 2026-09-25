//go:build !linux

package capture

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported is returned where there is no packet socket.
var ErrUnsupported = errors.New("packet capture needs Linux")

// Run fails: packet capture needs a Linux packet socket.
func Run(context.Context, string, func([]byte, time.Time)) error {
	return ErrUnsupported
}
