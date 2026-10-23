package workflow

import (
	"math"
	"strings"
)

const (
	MaxStageChain = 3
	StageChainKey = "_stage_chain"
)

type StageEntry struct {
	MoveID       string
	WorkspaceID  string
	EntryID      string
	EntryType    string
	StageID      string
	ByWorkflowID string
	ByChain      int
}

func StageAddedEvent(e StageEntry) (TriggerEvent, bool) {
	moveID, stageID := strings.TrimSpace(e.MoveID), strings.TrimSpace(e.StageID)
	if moveID == "" || stageID == "" || strings.TrimSpace(e.WorkspaceID) == "" || strings.TrimSpace(e.EntryID) == "" {
		return TriggerEvent{}, false
	}
	causedBy := strings.TrimSpace(e.ByWorkflowID)
	chain := 0
	if causedBy != "" {
		chain = e.ByChain + 1
	}
	if chain > MaxStageChain || chain < 0 {
		return TriggerEvent{}, false
	}
	data := StageAddedRunData(stageID)
	if chain > 0 {
		data[StageChainKey] = chain
	}
	return TriggerEvent{
		WorkspaceID: e.WorkspaceID,
		EntryID:     e.EntryID,
		EntryType:   e.EntryType,
		TriggerType: TriggerStageAdded,
		Data:        data,
		Occurrence:  moveID,
		CausedBy:    causedBy,
	}, true
}

func (e TriggerEvent) MayStart(workflowID string) bool {
	return e.CausedBy == "" || e.CausedBy != workflowID
}

func StageChainOf(state RunState) int {
	raw, present := state.Get(StageChainKey)
	if !present || raw == nil {
		return 0
	}
	var chain float64
	switch n := raw.(type) {
	case int:
		chain = float64(n)
	case int64:
		chain = float64(n)
	case float64:
		chain = n
	default:
		return MaxStageChain
	}
	if chain < 0 || chain != math.Trunc(chain) || chain > MaxStageChain {
		return MaxStageChain
	}
	return int(chain)
}

func stageOccurrenceKey(e TriggerEvent) string {
	occurrence := strings.TrimSpace(e.Occurrence)
	if occurrence == "" {
		return ""
	}
	return "stage/" + occurrence
}
