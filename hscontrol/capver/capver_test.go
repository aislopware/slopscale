package capver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTailscaleLatestMajorMinor(t *testing.T) {
	for _, test := range tailscaleLatestMajorMinorTests {
		t.Run("", func(t *testing.T) {
			output := TailscaleLatestMajorMinor(test.n, test.stripV)
			if diff := cmp.Diff(output, test.expected); diff != "" {
				t.Errorf("TailscaleLatestMajorMinor(%d, %v) mismatch (-want +got):\n%s", test.n, test.stripV, diff)
			}
		})
	}
}

func TestCapVerMinimumTailscaleVersion(t *testing.T) {
	for _, test := range capVerMinimumTailscaleVersionTests {
		t.Run("", func(t *testing.T) {
			output := TailscaleVersion(test.input)
			if output != test.expected {
				t.Errorf("CapVerFromTailscaleVersion(%d) = %s; want %s", test.input, output, test.expected)
			}
		})
	}
}

// TestCanOldCodeBeCleanedUp fails the regeneration that raises
// MinSupportedCapabilityVersion past a compat threshold, so the old path is
// deleted in that pull request instead of the server panicking at startup.
func TestCanOldCodeBeCleanedUp(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CanOldCodeBeCleanedUp: %v", r)
		}
	}()

	CanOldCodeBeCleanedUp()
}
