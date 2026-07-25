package telephony

import "time"

// SeatState is the live color/state of a human concurrency seat.
type SeatState string

const (
	SeatOffline SeatState = "offline"
	SeatFree    SeatState = "free"
	SeatRinging SeatState = "ringing"
	SeatOnCall  SeatState = "on_call"
	SeatWrapUp  SeatState = "wrap_up"
)

// HumanSeat is one human agent square on the live board.
type HumanSeat struct {
	UserID     string    `json:"user_id"`
	Username   string    `json:"username,omitempty"`
	State      SeatState `json:"state"`
	HasBrowser bool      `json:"has_browser"`
	HasBranch  bool      `json:"has_branch"`
	Since      time.Time `json:"since,omitempty"`
}

// Capacity is workspace concurrent call usage vs plan limit.
type Capacity struct {
	Used int64 `json:"used"`
	Max  int64 `json:"max"`
	// Pct is used/max*100 when max > 0.
	Pct float64 `json:"pct"`
}

// QueueStrip is optional live ACD depth (best-effort from Redis).
type QueueStrip struct {
	Depth     int64 `json:"depth"`
	Available bool  `json:"available"`
}

// BoardSnapshot is the live concurrency board (Redis / memory only — no SQL).
type BoardSnapshot struct {
	WorkspaceID string      `json:"workspace_id"`
	Rev         int64       `json:"rev"`
	AsOf        time.Time   `json:"as_of"`
	Capacity    Capacity    `json:"capacity"`
	Humans      []HumanSeat `json:"humans"`
	Queue       QueueStrip  `json:"queue"`
	// Online/Free/InCall summary for chips
	Online  int64    `json:"online"`
	Free    int64    `json:"free"`
	InCall  int64    `json:"in_call"`
	Ringing int64    `json:"ringing"`
	IdlePct *float64 `json:"idle_pct,omitempty"` // free/online when online > 0
}

// BoardStore is the Redis-backed live board (paint path never hits Postgres).
type BoardStore interface {
	// SaveHumans replaces the human seat set for the workspace and bumps rev.
	SaveHumans(workspaceID string, seats []HumanSeat) error
	// SetCapacity stores used/max for the workspace bar.
	SetCapacity(workspaceID string, used, max int64) error
	// SetQueueDepth stores optional live queue depth.
	SetQueueDepth(workspaceID string, depth int64) error
	// Get returns the full snapshot (assembles from Redis hashes).
	Get(workspaceID string) (*BoardSnapshot, error)
}

// CapacityReader reads concurrent call usage from CallSlotManager / plan.
type CapacityReader interface {
	// Snapshot returns used slots and max for workspace (max may be 0 if unknown).
	Snapshot(workspaceID string) (used, max int64, err error)
}

// GetBoardUseCase returns the live board (Redis only).
type GetBoardUseCase interface {
	Execute(workspaceID string) (*BoardSnapshot, error)
}

// BoardSync rebuilds human seats from dialer presence (called on presence change).
type BoardSync interface {
	SyncHumansFromPresence(workspaceID string, seats []HumanSeat, used, max int64) (*BoardSnapshot, error)
}
