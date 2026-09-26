package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The patterns ignore indentation, so the fixture is flattened to fit the
// line length limit.
const bunFixture = `bun = prev.bun.overrideAttrs (finalAttrs: _: {
  version = "1.4.2";
  __intentionallyOverridingVersion = true;
  passthru.sources = {
    "aarch64-darwin" = prev.fetchurl {
      url = "https://github.com/oven-sh/bun/releases/download/bun-v${finalAttrs.version}/bun-darwin-aarch64.zip";
      hash = "sha256-darwin=";
    };
    "x86_64-linux" = prev.fetchurl {
      url = "https://github.com/oven-sh/bun/releases/download/bun-v${finalAttrs.version}/bun-linux-x64-baseline.zip";
      hash = "sha256-linux=";
    };
  };
});
`

func TestBunPin(t *testing.T) {
	got, err := pinnedBun(bunFixture)
	if err != nil {
		t.Fatalf("pinnedBun: %v", err)
	}

	if got != "1.4.2" {
		t.Errorf("pinnedBun = %q, want 1.4.2", got)
	}

	want := []string{"bun-darwin-aarch64", "bun-linux-x64-baseline"}
	if diff := cmp.Diff(want, bunAssets(bunFixture)); diff != "" {
		t.Errorf("bunAssets mismatch (-want +got):\n%s", diff)
	}

	_, err = pinnedBun(`version = "1.4.2";`)
	if !errors.Is(err, errNoBunPin) {
		t.Errorf("pinnedBun without the override marker: error = %v, want %v", err, errNoBunPin)
	}
}

// Only the named asset's hash may move; the other systems keep theirs until
// they are hashed in turn.
func TestBunRewrite(t *testing.T) {
	got := setBunHash(bunFixture, "bun-linux-x64-baseline", "sha256-new=")
	got = setBunVersion(got, "1.5.0")

	for _, want := range []string{
		`version = "1.5.0";`,
		`hash = "sha256-darwin=";`,
		`hash = "sha256-new=";`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewrite is missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "sha256-linux=") {
		t.Errorf("rewrite kept the old linux hash:\n%s", got)
	}
}

// The real flake is what the area edits; a reformat that breaks the patterns
// would otherwise only show up as a dropped area.
func TestRepoBunPin(t *testing.T) {
	flake := string(mustRead(t, "../../flake.nix"))

	_, err := pinnedBun(flake)
	if err != nil {
		t.Fatalf("flake.nix: %v", err)
	}

	const systems = 3
	if got := bunAssets(flake); len(got) != systems {
		t.Errorf("flake.nix bun assets = %v, want one per system (%d)", got, systems)
	}
}
