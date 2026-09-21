package queue_event

import "time"

type Event struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	TransferID  string    `json:"transferId,omitempty"`
	CallID      string    `json:"callId,omitempty"`
	TargetKind  string    `json:"targetKind,omitempty"`
	TargetID    string    `json:"targetId,omitempty"`
	Type        string    `json:"type"`
	Position    int       `json:"position"`
	WaitedMS    int64     `json:"waitedMs"`
	OccurredAt  time.Time `json:"occurredAt"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Repository interface {
	Create(e *Event) error
	Stats(workspaceID string, from, to *time.Time) (*Stats, error)
	StatsWithSL(workspaceID string, from, to *time.Time, slSeconds int) (*Stats, error)
}

type Stats struct {
	Enqueued          int64   `json:"enqueued"`
	Connected         int64   `json:"connected"`
	Abandoned         int64   `json:"abandoned"`
	Overflow          int64   `json:"overflow"`
	QueueFull         int64   `json:"queue_full"`
	Cancelled         int64   `json:"cancelled"`
	AvgASAMs          float64 `json:"avg_asa_ms"`
	MaxWaitMS         float64 `json:"max_wait_ms"`
	ConnectedWithinSL int64   `json:"connected_within_sl"`
	ServiceLevelPct   float64 `json:"service_level_pct"`
	AbandonRate       float64 `json:"abandon_rate"`
}

type StatsOptions struct {
	ServiceLevelSeconds int
}
