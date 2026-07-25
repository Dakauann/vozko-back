package notification

import "time"

// OwnerEmailResolver resolves a workspace to its owner's email address.
type OwnerEmailResolver interface {
	OwnerEmail(workspaceID string) (string, error)
}

// Notification is a single transactional email request. Set WorkspaceID to send to
// the workspace owner, or Email to address a recipient directly. A non-empty
// DedupKey makes the send at-most-once for that logical event (see Dedup).
type Notification struct {
	WorkspaceID  string
	Email        string
	Subject      string
	Template     string
	Placeholders map[string]interface{}
	DedupKey     string
	DedupTTL     time.Duration
}

// Notifier is the single seam use cases call to send transactional email. It
// resolves the recipient, enforces idempotency, and publishes to the async email
// queue, so callers never duplicate that wiring.
type Notifier interface {
	Notify(n Notification) error
}
