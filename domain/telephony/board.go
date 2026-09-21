package telephony

import "time"

type SeatState string

const (
	SeatOffline SeatState = "offline"
	SeatFree    SeatState = "free"
	SeatRinging SeatState = "ringing"
	SeatOnCall  SeatState = "on_call"
	SeatWrapUp  SeatState = "wrap_up"
)

type HumanSeat struct {
	UserID     string    `json:"user_id"`
	Username   string    `json:"username,omitempty"`
	State      SeatState `json:"state"`
	HasBrowser bool      `json:"has_browser"`
	Since      time.Time `json:"since,omitempty"`
}

type Capacity struct {
	Used int64   `json:"used"`
	Max  int64   `json:"max"`
	Pct  float64 `json:"pct"`
}

type QueueStrip struct {
	Depth     int64 `json:"depth"`
	Available bool  `json:"available"`
}

type BoardSnapshot struct {
	WorkspaceID string      `json:"workspace_id"`
	Rev         int64       `json:"rev"`
	AsOf        time.Time   `json:"as_of"`
	Capacity    Capacity    `json:"capacity"`
	Humans      []HumanSeat `json:"humans"`
	Queue       QueueStrip  `json:"queue"`
	Online      int64       `json:"online"`
	Free        int64       `json:"free"`
	InCall      int64       `json:"in_call"`
	Ringing     int64       `json:"ringing"`
	IdlePct     *float64    `json:"idle_pct,omitempty"`
}

type BoardStore interface {
	SaveHumans(workspaceID string, seats []HumanSeat) error
	SetCapacity(workspaceID string, used, max int64) error
	SetQueueDepth(workspaceID string, depth int64) error
	Get(workspaceID string) (*BoardSnapshot, error)
}

type CapacityReader interface {
	Snapshot(workspaceID string) (used, max int64, err error)
}

type GetBoardUseCase interface {
	Execute(workspaceID string) (*BoardSnapshot, error)
}

type BoardSync interface {
	SyncHumansFromPresence(workspaceID string, seats []HumanSeat, used, max int64) (*BoardSnapshot, error)
}
