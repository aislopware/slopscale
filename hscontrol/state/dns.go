package state

import (
	"fmt"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
)

// DNSStatus is the DNS configuration as the API reports it: what the
// tailnet runs with, where it comes from and what the file says.
type DNSStatus struct {
	// MagicDNS and BaseDomain come from the config file and cannot change
	// at runtime.
	MagicDNS   bool
	BaseDomain string
	// Effective is what clients receive.
	Effective types.DNSSettings
	// FromFile is the config file's dns section, what a reset returns to.
	FromFile types.DNSSettings
	// Overridden reports whether the settings table holds an override.
	Overridden bool
	// ExtraRecordsPath is dns.extra_records_path when set. Then the file
	// owns the extra records and the override's records are ignored.
	ExtraRecordsPath string
}

// DNS reports the DNS configuration in force.
func (s *State) DNS() DNSStatus {
	file := s.cfg.DNSConfig
	effective := s.cfg.EffectiveDNS()

	return DNSStatus{
		MagicDNS:         file.MagicDNS,
		BaseDomain:       file.BaseDomain,
		Effective:        effective.Settings(),
		FromFile:         file.Settings(),
		Overridden:       s.cfg.DNSOverride() != nil,
		ExtraRecordsPath: file.ExtraRecordsPath,
	}
}

// SetDNS replaces the runtime DNS settings. The settings are normalized
// and validated, stored, applied to the map responses and pushed to every
// client. Extra records cannot be set here while dns.extra_records_path
// owns them.
func (s *State) SetDNS(settings types.DNSSettings) (DNSStatus, change.Change, error) {
	settings = settings.Normalize()

	err := settings.Validate()
	if err != nil {
		return DNSStatus{}, change.Change{}, err
	}

	if s.cfg.DNSConfig.ExtraRecordsPath != "" && len(settings.ExtraRecords) > 0 {
		return DNSStatus{}, change.Change{}, types.ErrDNSExtraRecordsFromFile
	}

	err = s.db.SaveDNSSettings(settings)
	if err != nil {
		return DNSStatus{}, change.Change{}, fmt.Errorf("saving dns settings: %w", err)
	}

	s.cfg.SetDNSOverride(&settings)

	return s.DNS(), change.DNSConfig(), nil
}

// ResetDNS drops the runtime DNS settings so the config file is in force
// again, and pushes the result to every client.
func (s *State) ResetDNS() (DNSStatus, change.Change, error) {
	err := s.db.DeleteDNSSettings()
	if err != nil {
		return DNSStatus{}, change.Change{}, fmt.Errorf("deleting dns settings: %w", err)
	}

	s.cfg.SetDNSOverride(nil)

	return s.DNS(), change.DNSConfig(), nil
}

// loadDNS applies the stored override, if any, when the server starts.
func (s *State) loadDNS() error {
	settings, err := s.db.LoadDNSSettings()
	if err != nil {
		return err
	}

	s.cfg.SetDNSOverride(settings)

	return nil
}
