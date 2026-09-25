package dnsproxy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procNetDir holds the kernel's socket tables; tests point it elsewhere.
var procNetDir = "/proc/net"

// socket states in /proc/net/{tcp,udp}{,6}.
const (
	tcpListen = "0A"
	udpBound  = "07"
)

// wildcardListener reports whether a socket already serves port on every
// address (0.0.0.0 or ::), and which table it is in. Outside Linux the
// tables do not exist and nothing is reported.
func wildcardListener(dir string, port uint16) (string, bool) {
	tables := []struct{ file, state string }{
		{"udp", udpBound}, {"udp6", udpBound}, {"tcp", tcpListen}, {"tcp6", tcpListen},
	}

	for _, table := range tables {
		if wildcardIn(filepath.Join(dir, table.file), table.state, port) {
			return fmt.Sprintf("%s *:%d", strings.TrimSuffix(table.file, "6"), port), true
		}
	}

	return "", false
}

// wildcardIn scans one table for an unspecified local address on port in
// the given state. Its lines look like
// "0: 00000000:0035 00000000:0000 07 ...", addresses in hex.
func wildcardIn(path, state string, port uint16) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())

		const localField, stateField = 1, 3
		if len(fields) <= stateField || fields[stateField] != state {
			continue
		}

		host, hexPort, ok := strings.Cut(fields[localField], ":")
		if !ok || strings.Trim(host, "0") != "" {
			continue
		}

		p, err := strconv.ParseUint(hexPort, 16, 16)
		if err == nil && p == uint64(port) {
			return true
		}
	}

	return false
}
