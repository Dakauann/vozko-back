// Package agent_presence stores durable online/on_call intervals for human attendants.
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
	Source      string     `json:"source"` // ws_hub | call_session
	StartedAt   time.Time  `json:"startedAt"`
	EndedAt     *time.Time `json:"endedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type Repository interface {
	// Transition closes any open interval for the user in the workspace and opens a new one when state is not offline.
	// Offline ends the open interval without opening a new row.
	Transition(workspaceID, userID string, state State, source string, at time.Time) error
	// Occupancy returns on_call_ms / online_ms for the window.
	Occupancy(workspaceID string, from, to *time.Time) ([]OccupancyRow, error)
	// LastSeen returns, per user, the last moment they were present in the
	// workspace. Users with no presence history are ABSENT from the map rather
	// than zero-valued, so "never online" stays distinguishable from "online at
	// the zero time".
	//
	// An interval that is still open is reported at its started_at, never at
	// now(): a replica that dies without unregistering leaves ended_at NULL
	// forever, and reading that as "online right now" would make a crashed
	// process's last user permanently the freshest candidate in the roulette.
	// started_at is a lower bound that is always true, and a user who really is
	// online is covered by the live connected-set overlay in the resolver, so
	// nothing is lost by being conservative here.
	LastSeen(workspaceID string, userIDs []string) (map[string]time.Time, error)
}

type OccupancyRow struct {
	UserID    string  `json:"user_id"`
	OnlineMS  int64   `json:"online_ms"`
	OnCallMS  int64   `json:"on_call_ms"`
	Occupancy float64 `json:"occupancy"`
}
