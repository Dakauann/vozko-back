package sqlexpr

const (
	LeaseExpired = "(heartbeat_at IS NULL OR heartbeat_at < ?)"
	LeaseFree    = "(claim_token IS NULL OR " + LeaseExpired + ")"
)
