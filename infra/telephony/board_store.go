package telephony_infra

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/telephony"
)

const boardTTL = 90 * time.Second

type boardStore struct {
	shared cache.SharedState
}

// NewBoardStore returns a Redis-backed live board store.
func NewBoardStore(shared cache.SharedState) telephony.BoardStore {
	return &boardStore{shared: shared}
}

func humansKey(ws string) string   { return "board:humans:" + ws }
func aiKey(ws string) string       { return "board:ai:" + ws }
func capacityKey(ws string) string { return "board:capacity:" + ws }
func queueKey(ws string) string    { return "board:queue:" + ws }
func revKey(ws string) string      { return "board:rev:" + ws }

func (s *boardStore) bumpRev(ws string) int64 {
	if s == nil || s.shared == nil {
		return 0
	}
	n, _ := s.shared.Incr(revKey(ws))
	_, _ = s.shared.Expire(revKey(ws), boardTTL)
	return n
}

func (s *boardStore) touch(ws string) {
	if s == nil || s.shared == nil {
		return
	}
	_, _ = s.shared.Expire(humansKey(ws), boardTTL)
	_, _ = s.shared.Expire(aiKey(ws), boardTTL)
	_, _ = s.shared.Expire(capacityKey(ws), boardTTL)
	_, _ = s.shared.Expire(queueKey(ws), boardTTL)
	_, _ = s.shared.Expire(revKey(ws), boardTTL)
}

func (s *boardStore) SaveHumans(workspaceID string, seats []telephony.HumanSeat) error {
	if s == nil || s.shared == nil || strings.TrimSpace(workspaceID) == "" {
		return nil
	}
	key := humansKey(workspaceID)
	// Replace set: delete key then rewrite (small N for workspace agents).
	_ = s.shared.Del(key)
	for _, seat := range seats {
		if seat.UserID == "" {
			continue
		}
		b, err := json.Marshal(seat)
		if err != nil {
			continue
		}
		_ = s.shared.HSet(key, seat.UserID, string(b))
	}
	_, _ = s.shared.Expire(key, boardTTL)
	s.bumpRev(workspaceID)
	s.touch(workspaceID)
	return nil
}

func (s *boardStore) SetCapacity(workspaceID string, used, max int64) error {
	if s == nil || s.shared == nil || workspaceID == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]int64{"used": used, "max": max})
	_ = s.shared.SetString(capacityKey(workspaceID), string(payload), boardTTL)
	s.bumpRev(workspaceID)
	return nil
}

func (s *boardStore) SetQueueDepth(workspaceID string, depth int64) error {
	if s == nil || s.shared == nil || workspaceID == "" {
		return nil
	}
	if depth < 0 {
		depth = 0
	}
	_ = s.shared.SetString(queueKey(workspaceID), strconv.FormatInt(depth, 10), boardTTL)
	s.bumpRev(workspaceID)
	return nil
}

func (s *boardStore) Get(workspaceID string) (*telephony.BoardSnapshot, error) {
	out := &telephony.BoardSnapshot{
		WorkspaceID: workspaceID,
		AsOf:        time.Now().UTC(),
		Humans:      []telephony.HumanSeat{},
	}
	if s == nil || s.shared == nil || workspaceID == "" {
		return out, nil
	}

	if revStr, _ := s.shared.GetString(revKey(workspaceID)); revStr != "" {
		if n, err := strconv.ParseInt(revStr, 10, 64); err == nil {
			out.Rev = n
		}
	}

	if capRaw, _ := s.shared.GetString(capacityKey(workspaceID)); capRaw != "" {
		var c struct {
			Used int64 `json:"used"`
			Max  int64 `json:"max"`
		}
		if json.Unmarshal([]byte(capRaw), &c) == nil {
			out.Capacity.Used = c.Used
			out.Capacity.Max = c.Max
			if c.Max > 0 {
				out.Capacity.Pct = math.Round(float64(c.Used)/float64(c.Max)*10000) / 100
			}
		}
	}

	if qRaw, _ := s.shared.GetString(queueKey(workspaceID)); qRaw != "" {
		if d, err := strconv.ParseInt(qRaw, 10, 64); err == nil {
			out.Queue = telephony.QueueStrip{Depth: d, Available: true}
		}
	}

	humans, _ := s.shared.HGetAll(humansKey(workspaceID))
	for _, raw := range humans {
		var seat telephony.HumanSeat
		if json.Unmarshal([]byte(raw), &seat) != nil || seat.UserID == "" {
			continue
		}
		out.Humans = append(out.Humans, seat)
		out.Online++
		switch seat.State {
		case telephony.SeatFree:
			out.Free++
		case telephony.SeatOnCall:
			out.InCall++
		case telephony.SeatRinging:
			out.Ringing++
		}
	}
	sort.Slice(out.Humans, func(i, j int) bool {
		// busy first, then name
		pi, pj := seatPrio(out.Humans[i].State), seatPrio(out.Humans[j].State)
		if pi != pj {
			return pi < pj
		}
		return out.Humans[i].Username < out.Humans[j].Username ||
			(out.Humans[i].Username == out.Humans[j].Username && out.Humans[i].UserID < out.Humans[j].UserID)
	})
	if out.Online > 0 {
		idle := math.Round(float64(out.Free)/float64(out.Online)*10000) / 100
		out.IdlePct = &idle
	}

	return out, nil
}

func seatPrio(s telephony.SeatState) int {
	switch s {
	case telephony.SeatOnCall:
		return 0
	case telephony.SeatRinging:
		return 1
	case telephony.SeatWrapUp:
		return 2
	case telephony.SeatFree:
		return 3
	default:
		return 4
	}
}
