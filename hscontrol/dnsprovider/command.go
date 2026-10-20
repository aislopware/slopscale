package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/libdns/libdns"
)

// commandTimeout bounds one run of the program.
const commandTimeout = 2 * time.Minute

var errCommandPath = errors.New("path is required")

// Command runs a program with the record's name and value as its two
// arguments, for zones the other providers cannot reach.
type Command struct {
	// Path publishes a record.
	Path string `json:"path"`
	// DeletePath removes one again, with the same arguments. Empty leaves
	// published records in the zone, which is how the provider worked
	// before it removed anything.
	DeletePath string `json:"delete_path,omitempty"`
}

// AppendRecords runs the program once per record; a non-zero exit is the
// error, with what the program printed.
func (c *Command) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	return c.run(ctx, c.Path, zone, recs)
}

// DeleteRecords runs the delete program once per record, if there is one.
func (c *Command) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	if c.DeletePath == "" {
		return nil, nil
	}

	return c.run(ctx, c.DeletePath, zone, recs)
}

func (c *Command) validate() error {
	if c.Path == "" {
		return errCommandPath
	}

	return nil
}

func (c *Command) run(ctx context.Context, path, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	txts, err := txtRecords(zone, recs)
	if err != nil {
		return nil, err
	}

	done := make([]libdns.Record, 0, len(recs))

	for i, txt := range txts {
		err := runCommand(ctx, path, txt[0], txt[1])
		if err != nil {
			return done, err
		}

		done = append(done, recs[i])
	}

	return done, nil
}

func runCommand(ctx context.Context, path, name, value string) error {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, name, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("running %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}

	return nil
}
