package dns

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestExtraRecordsFileIsReadOnceItSettles covers aislopware/slopscale#2753
// and #2782: a file written in several steps is read once, after the
// writer has gone quiet, so no partial write is parsed, and the names it
// carries are lowercased like those from the API.
func TestExtraRecordsFileIsReadOnceItSettles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "extra.json")
	require.NoError(t, os.WriteFile(path,
		[]byte(`[{"name":"a.example.com","type":"A","value":"100.64.0.1"}]`), 0o600))

	er, err := NewExtraRecordsManager(path)
	require.NoError(t, err)

	t.Cleanup(er.Close)

	go er.Run()

	// A truncate, a partial write and the rest, the way an editor or a
	// redirect lands on disk; only the last is valid JSON.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`[{"name":"Printer.Fritz.Box.","type":"a",`)
	require.NoError(t, err)
	_, err = f.WriteString(`"value":"192.168.178.1"}]`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	select {
	case records := <-er.UpdateCh():
		assert.Equal(t, []tailcfg.DNSRecord{{Name: "printer.fritz.box", Type: "A", Value: "192.168.178.1"}}, records)
	case <-time.After(5 * time.Second):
		t.Fatal("no update after the file settled")
	}

	select {
	case records := <-er.UpdateCh():
		t.Fatalf("a second update for the same write: %v", records)
	case <-time.After(3 * extraRecordsSettle):
	}
}

// A records file is held to the settings API's rule: a bad file refuses to
// start, and a bad edit to a running file keeps the records already served
// rather than handing clients a name they panic on.
func TestExtraRecordsFileRejectsInvalidRecords(t *testing.T) {
	t.Parallel()

	const bad = `[{"name":"evil.com\nnameserver 1.1.1.1","type":"A","value":"100.64.0.1"}]`

	t.Run("at-start", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "extra.json")
		require.NoError(t, os.WriteFile(path, []byte(bad), 0o600))

		_, err := NewExtraRecordsManager(path)
		require.ErrorIs(t, err, types.ErrDNSDomainInvalid)
	})

	t.Run("on-reload", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "extra.json")
		require.NoError(t, os.WriteFile(path,
			[]byte(`[{"name":"a.example.com","type":"A","value":"100.64.0.1"}]`), 0o600))

		er, err := NewExtraRecordsManager(path)
		require.NoError(t, err)

		t.Cleanup(er.Close)

		go er.Run()

		require.NoError(t, os.WriteFile(path, []byte(bad), 0o600))

		select {
		case records := <-er.UpdateCh():
			t.Fatalf("an invalid file was served: %v", records)
		case <-time.After(5 * extraRecordsSettle):
		}

		assert.Equal(t, []tailcfg.DNSRecord{{Name: "a.example.com", Type: "A", Value: "100.64.0.1"}}, er.Records())
	})
}
