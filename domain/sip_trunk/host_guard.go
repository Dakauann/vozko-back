package sip_trunk

// HostGuard is a domain port for resolve-time host validation. DNS resolution is
// I/O, so it cannot live in the pure SIPTrunk.Validate(); the use cases call this
// port (implemented in infra) after validation to reject a hostname that resolves
// to an internal/blocked address. A nil guard disables the check.
type HostGuard interface {
	// ResolvesToBlocked reports whether host (a hostname or IP literal) points at
	// a blocked internal address (see IsBlockedIP). A host that cannot be resolved
	// is reported as not blocked, it cannot be used to reach an internal service.
	ResolvesToBlocked(host string) bool
}
