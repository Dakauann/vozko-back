package notification

import "time"

type OwnerEmailResolver interface {
	OwnerEmail(workspaceID string) (string, error)
}

type Notification struct {
	WorkspaceID  string
	Email        string
	Subject      string
	Template     string
	Placeholders map[string]interface{}
	DedupKey     string
	DedupTTL     time.Duration
}

type Notifier interface {
	Notify(n Notification) error
}
