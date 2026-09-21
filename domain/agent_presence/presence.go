package agent_presence

import "time"

type State string

const (
	StateOnline  State = "online"
	StateOffline State = "offline"
	StateOnCall  State = "on_call"
	StateWrapUp  State = "wrap_up"
)

func (s State) Valid() bool {
	switch s {
	case StateOnline, StateOffline, StateOnCall, StateWrapUp:
		return true
	}
	return false
}

type Interval struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	UserID      string     `json:"userId"`
	State       State      `json:"state"`
	Source      string     `json:"source"`
	StartedAt   time.Time  `json:"startedAt"`
	EndedAt     *time.Time `json:"endedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type Repository interface {
	Transition(workspaceID, userID string, state State, source string, at time.Time) error
	Occupancy(workspaceID string, from, to *time.Time) ([]OccupancyRow, error)
	LastSeen(workspaceID string, userIDs []string) (map[string]time.Time, error)
}

type OccupancyRow struct {
	UserID    string  `json:"user_id"`
	OnlineMS  int64   `json:"online_ms"`
	OnCallMS  int64   `json:"on_call_ms"`
	Occupancy float64 `json:"occupancy"`
}
