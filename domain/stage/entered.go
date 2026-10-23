package stage

import "strings"

type MoveCause struct {
	WorkflowID string `json:"workflow_id,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	Chain      int    `json:"chain,omitempty"`
}

type Entered struct {
	MoveID      string    `json:"move_id"`
	WorkspaceID string    `json:"workspace_id"`
	EntryID     string    `json:"entry_id"`
	EntryType   string    `json:"entry_type"`
	StageID     string    `json:"stage_id"`
	FromStageID string    `json:"from_stage_id,omitempty"`
	Cause       MoveCause `json:"cause"`
}

type EnteredListener interface {
	StageEntered(Entered)
}

func EnteredBy(placed *EntryStage, previousStageID string) (Entered, bool) {
	if placed == nil || strings.TrimSpace(placed.StageID) == "" || placed.StageID == previousStageID {
		return Entered{}, false
	}
	return Entered{
		MoveID:      placed.ID,
		WorkspaceID: placed.WorkspaceID,
		EntryID:     placed.EntryID,
		EntryType:   placed.EntryType,
		StageID:     placed.StageID,
		FromStageID: previousStageID,
		Cause:       placed.Cause,
	}, true
}
