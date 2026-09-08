package state

import "github.com/juanfont/headscale/hscontrol/types"

// ExportAuditEvents hands every audit entry matching q to fn, oldest
// first, read in batches so a large export stays out of memory. The store
// caps how many an export returns.
func (s *State) ExportAuditEvents(q types.AuditQuery, fn func(*types.AuditEvent) error) error {
	return s.db.ExportAuditEvents(q, fn)
}
