package hscontrol

import (
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/util/zstdframe"
)

// decodeMapFrame checks the length prefix of one map response on the wire
// and returns the message it carries, decoded the way the client does.
func decodeMapFrame(t *testing.T, frame []byte, compressed bool) tailcfg.MapResponse {
	t.Helper()

	require.GreaterOrEqual(t, len(frame), reservedResponseHeaderSize)

	body := frame[reservedResponseHeaderSize:]
	require.Len(t, body, int(binary.LittleEndian.Uint32(frame)))

	if compressed {
		var err error

		body, err = zstdframe.AppendDecode(nil, body)
		require.NoError(t, err)
	}

	var msg tailcfg.MapResponse

	require.NoError(t, json.Unmarshal(body, &msg))

	return msg
}

func TestWriteMapFramesBody(t *testing.T) {
	t.Parallel()

	msg := &tailcfg.MapResponse{
		Node:      &tailcfg.Node{Name: "node.example.ts.net."},
		DNSConfig: &tailcfg.DNSConfig{Domains: []string{"example.ts.net"}},
	}

	for _, compress := range []string{"", util.ZstdCompression} {
		t.Run("compress="+compress, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			session := &mapSession{
				req:  tailcfg.MapRequest{Compress: compress},
				w:    rec,
				node: (&types.Node{}).View(),
				log:  zerolog.Nop(),
			}

			require.NoError(t, session.writeMap(msg))

			got := decodeMapFrame(t, rec.Body.Bytes(), compress == util.ZstdCompression)
			assert.Equal(t, msg.Node.Name, got.Node.Name)
			assert.Equal(t, msg.DNSConfig.Domains, got.DNSConfig.Domains)
		})
	}
}

// TestWriteMapConcurrentSessions exercises the pooled encode buffers from
// many sessions at once; each frame must still carry its own message.
func TestWriteMapConcurrentSessions(t *testing.T) {
	t.Parallel()

	const sessions = 16

	var wg sync.WaitGroup

	for i := range sessions {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			session := &mapSession{
				req:  tailcfg.MapRequest{Compress: util.ZstdCompression},
				w:    rec,
				node: (&types.Node{}).View(),
				log:  zerolog.Nop(),
			}
			name := string(rune('a'+i)) + ".example.ts.net."

			for range 50 {
				rec.Body.Reset()
				require.NoError(t, session.writeMap(&tailcfg.MapResponse{Node: &tailcfg.Node{Name: name}}))
				assert.Equal(t, name, decodeMapFrame(t, rec.Body.Bytes(), true).Node.Name)
			}
		})
	}

	wg.Wait()
}
