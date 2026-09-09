package state

import (
	"fmt"

	"github.com/aislopware/slopscale/hscontrol/idtoken"
	"github.com/aislopware/slopscale/hscontrol/types/change"
)

// IDTokenSigner returns the identity token signer, loading the key from
// the settings table or making one on first use; every server on the
// same database ends up with the same key.
func (s *State) IDTokenSigner() (*idtoken.Signer, error) {
	if signer := s.idTokenSigner.Load(); signer != nil {
		return signer, nil
	}

	s.idTokenMu.Lock()
	defer s.idTokenMu.Unlock()

	if signer := s.idTokenSigner.Load(); signer != nil {
		return signer, nil
	}

	encoded, err := s.db.LoadIDTokenKey()
	if err != nil {
		return nil, err
	}

	if encoded == "" {
		encoded, err = s.newIDTokenKey()
		if err != nil {
			return nil, err
		}
	}

	key, err := idtoken.DecodeKey(encoded)
	if err != nil {
		return nil, fmt.Errorf("stored identity token key: %w", err)
	}

	signer, err := idtoken.New(key)
	if err != nil {
		return nil, err
	}

	s.idTokenSigner.Store(signer)

	return signer, nil
}

// newIDTokenKey makes a key and stores it unless another server got
// there first, returning the one in force.
func (s *State) newIDTokenKey() (string, error) {
	key, err := idtoken.GenerateKey()
	if err != nil {
		return "", err
	}

	fresh, err := idtoken.EncodeKey(key)
	if err != nil {
		return "", err
	}

	return s.db.EnsureIDTokenKey(fresh)
}

// LatestClientVersion is the latest stable Tailscale client release the
// server found at pkgs.tailscale.com, "1.86.2", or empty until the first
// lookup; see [tailcfg.MapResponse.ClientVersion].
func (s *State) LatestClientVersion() string {
	if v := s.latestClientVersion.Load(); v != nil {
		return *v
	}

	return ""
}

// SetLatestClientVersion records the latest release. When it changed,
// the returned change tells every node its self node again, which
// carries the client version notice; the bool says whether it changed.
func (s *State) SetLatestClientVersion(version string) (change.Change, bool) {
	if version == "" || version == s.LatestClientVersion() {
		return change.Change{}, false
	}

	s.latestClientVersion.Store(&version)

	return change.Change{Reason: "latest client version", IncludeSelf: true}, true
}
