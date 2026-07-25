package telephony_usecase

import (
	"strings"
	"time"

	"vozko/domain/telephony"
)

type boardService struct {
	store    telephony.BoardStore
	capacity telephony.CapacityReader
}

// NewBoardService builds live board sync + get use cases.
func NewBoardService(store telephony.BoardStore, capacity telephony.CapacityReader) *boardService {
	return &boardService{store: store, capacity: capacity}
}

func (s *boardService) Execute(workspaceID string) (*telephony.BoardSnapshot, error) {
	if s == nil || s.store == nil {
		return &telephony.BoardSnapshot{
			WorkspaceID: workspaceID,
			AsOf:        time.Now().UTC(),
			Humans:      []telephony.HumanSeat{},
		}, nil
	}
	// Refresh capacity from slot manager when available (best-effort).
	if s.capacity != nil && workspaceID != "" {
		used, max, err := s.capacity.Snapshot(workspaceID)
		if err == nil {
			_ = s.store.SetCapacity(workspaceID, used, max)
		}
	}
	return s.store.Get(workspaceID)
}

// SyncHumansFromPresence rebuilds human seats and capacity after dialer presence change.
func (s *boardService) SyncHumansFromPresence(workspaceID string, seats []telephony.HumanSeat, used, max int64) (*telephony.BoardSnapshot, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	ws := strings.TrimSpace(workspaceID)
	if ws == "" {
		return nil, nil
	}
	now := time.Now().UTC()
	for i := range seats {
		if seats[i].Since.IsZero() {
			seats[i].Since = now
		}
	}
	_ = s.store.SaveHumans(ws, seats)
	if max > 0 || used > 0 {
		_ = s.store.SetCapacity(ws, used, max)
	} else if s.capacity != nil {
		if u, m, err := s.capacity.Snapshot(ws); err == nil {
			_ = s.store.SetCapacity(ws, u, m)
		}
	}
	return s.store.Get(ws)
}

// Ensure interfaces.
var (
	_ telephony.GetBoardUseCase = (*boardService)(nil)
	_ telephony.BoardSync       = (*boardService)(nil)
)
