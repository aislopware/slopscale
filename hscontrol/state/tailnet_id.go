package state

import (
	"crypto/rand"
	"fmt"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"tailscale.com/tailcfg"
)

// TailnetID returns the tailnet's stable ID: made on the server's first
// start, kept in the settings table and never changed afterwards. The
// mapper puts it on each node's self view, where the client reports it as
// CurrentTailnet.StableID, and /api/v2 accepts it as the tailnet in a path.
func (s *State) TailnetID() tailcfg.StableTailnetID {
	if s == nil {
		return ""
	}

	return s.tailnetID
}

// loadTailnetID reads the stored tailnet ID, or makes and stores one on
// a database that has none yet; of two servers starting together on one
// database, both end up with the ID the first one wrote.
func loadTailnetID(db *hsdb.HSDatabase) (tailcfg.StableTailnetID, error) {
	id, err := db.LoadTailnetID()
	if err != nil {
		return "", err
	}

	if id == "" {
		id, err = db.EnsureTailnetID(newTailnetID())
		if err != nil {
			return "", fmt.Errorf("storing the tailnet ID: %w", err)
		}
	}

	return tailcfg.StableTailnetID(id), nil
}

// Tailnet IDs take the hosted control plane's shape, "T" + an
// alphanumeric body + "CNTRL" (its docs show T123456CNTRL), so tooling
// that recognises Tailscale IDs treats this one alike. The body is ten
// random base62 characters, about 59 bits.
const (
	tailnetIDPrefix   = "T"
	tailnetIDSuffix   = "CNTRL"
	tailnetIDBodyLen  = 10
	tailnetIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// newTailnetID makes a random tailnet ID.
func newTailnetID() string {
	// A byte at or above the largest multiple of the alphabet's length is
	// drawn again, so every character is equally likely.
	limit := byte(256 / len(tailnetIDAlphabet) * len(tailnetIDAlphabet))
	body := make([]byte, 0, tailnetIDBodyLen)

	var b [1]byte
	for len(body) < tailnetIDBodyLen {
		_, _ = rand.Read(b[:])

		if b[0] < limit {
			body = append(body, tailnetIDAlphabet[int(b[0])%len(tailnetIDAlphabet)])
		}
	}

	return tailnetIDPrefix + string(body) + tailnetIDSuffix
}
