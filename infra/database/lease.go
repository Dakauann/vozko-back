package database

const LeaseFreeSQL = "(claim_token IS NULL OR heartbeat_at IS NULL OR heartbeat_at < ?)"
