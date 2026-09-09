package dnsprovider

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// commandTimeout bounds one run of the program.
const commandTimeout = 2 * time.Minute

// Command runs a program with the record's name and value as its two
// arguments, for zones the other providers cannot reach.
type Command struct {
	path string
}

// NewCommand builds the provider.
func NewCommand(cfg types.CommandDNSConfig) *Command {
	return &Command{path: cfg.Path}
}

// SetTXT runs the program; a non-zero exit is the error, with what the
// program printed.
func (c *Command) SetTXT(ctx context.Context, name, value string) error {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	//nolint:gosec // G204: the program is the operator's, the arguments a checked record name and a token
	out, err := exec.CommandContext(ctx, c.path, name, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("running %s: %w: %s", c.path, err, strings.TrimSpace(string(out)))
	}

	return nil
}
