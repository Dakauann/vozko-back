package telephony_usecase

import (
	"strconv"
	"strings"

	"vozko/domain/cache"
	"vozko/domain/telephony"
	workspace_domain "vozko/domain/workspace"
)

// SlotCapacityReader reads concurrent usage from Redis keys owned by CallSlotManager
// and resolves max via CallSlotManager when provided.
type SlotCapacityReader struct {
	Shared  cache.SharedState
	Slots   *workspace_domain.CallSlotManager
}

func NewSlotCapacityReader(shared cache.SharedState, slots *workspace_domain.CallSlotManager) *SlotCapacityReader {
	return &SlotCapacityReader{Shared: shared, Slots: slots}
}

func (r *SlotCapacityReader) Snapshot(workspaceID string) (used, max int64, err error) {
	ws := strings.TrimSpace(workspaceID)
	if ws == "" {
		return 0, 0, nil
	}
	if r.Shared != nil {
		key := "calls:active:count:ws:" + ws
		if s, e := r.Shared.GetString(key); e == nil && s != "" {
			if n, pe := strconv.ParseInt(s, 10, 64); pe == nil && n >= 0 {
				used = n
			}
		}
	}
	if r.Slots != nil {
		if m, e := r.Slots.WorkspaceLimit(ws); e == nil && m > 0 {
			max = m
		}
	}
	return used, max, nil
}

var _ telephony.CapacityReader = (*SlotCapacityReader)(nil)
