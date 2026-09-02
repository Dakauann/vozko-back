package stage

import "context"

type CreateStageUseCase interface {
	Execute(workspaceID string, input CreateStageInput) (*Stage, error)
}

type UpdateStageUseCase interface {
	Execute(workspaceID, StageID string, input UpdateStageInput) (*Stage, error)
}

type DeleteStageUseCase interface {
	Execute(workspaceID, StageID string) error
}

// ListStagesUseCase lists the stages of ONE conversation funnel.
//
// pipelineID names it directly and wins when set — that is how the CRM asks for
// the funnel the operator actually selected. Falling back to campaignID (or, with
// neither, the workspace default) is the legacy resolution: it is what made every
// stage list in the product show the default funnel's stages no matter which one
// was on screen.
type ListStagesUseCase interface {
	Execute(workspaceID, campaignID, campaignType, pipelineID string) ([]*Stage, error)
}

type SetInitialStageUseCase interface {
	Execute(workspaceID, StageID string) (*Stage, error)
}

type AssignEntryStageUseCase interface {
	Execute(workspaceID string, input AssignEntryStageInput) (*EntryStage, error)
}

type RemoveEntryStageUseCase interface {
	Execute(workspaceID string, input RemoveEntryStageInput) error
}

type GetEntryStageUseCase interface {
	Execute(workspaceID, entryID, entryType string) (*EntryStage, error)
}

type GetBatchEntryStagesUseCase interface {
	Execute(workspaceID string, entryIDs []string, entryType string) (map[string]*EntryStage, error)
}

type ReorderStagesUseCase interface {
	Execute(workspaceID string, input ReorderStagesInput) ([]*Stage, error)
}

type CreateStageInput struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Color        string `json:"color,omitempty"`
	CampaignID   string `json:"campaignId,omitempty"`
	CampaignType string `json:"campaignType,omitempty"`
	// PipelineID puts the stage on a named funnel. Without it the stage lands on
	// the workspace default, which is what made it impossible to add a column to a
	// custom funnel from the CRM.
	PipelineID string `json:"pipelineId,omitempty"`
}

type UpdateStageInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Color       *string `json:"color,omitempty"`
}

// AssignEntryStageInput moves one entry onto a stage.
//
// ActorID names who is doing it, in the stored actor-id form: a user uuid, an
// "ai:<agentID>" attendant, or "system"/empty for the platform. It is on the
// INPUT rather than resolved by the caller because the use case writes the
// timeline event, and an event with no actor is what the timeline showed while
// that write lived in the HTTP handler and every other caller skipped it.
type AssignEntryStageInput struct {
	StageID   string `json:"StageID"`
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
	ActorID   string `json:"-"`
}

// RemoveEntryStageInput takes an entry off a stage. See AssignEntryStageInput
// for ActorID; it is a struct for the same reason, so a new field cannot be
// silently dropped by a caller passing positional strings.
type RemoveEntryStageInput struct {
	StageID   string `json:"StageID"`
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
	ActorID   string `json:"-"`
}

type ReorderStagesInput struct {
	StageIDs     []string `json:"stageIds"`
	CampaignID   string   `json:"campaignId"`
	CampaignType string   `json:"campaignType"`
	// PipelineID scopes the list returned after reordering, so the caller gets the
	// funnel it just reordered rather than the default one.
	PipelineID string `json:"pipelineId,omitempty"`
}

type CreateStageGroupUseCase interface {
	Execute(ctx context.Context, workspaceID string, input CreateStageGroupInput) (*StageGroup, error)
}

type UpdateStageGroupUseCase interface {
	Execute(workspaceID, groupID string, input UpdateStageGroupInput) (*StageGroup, error)
}

type DeleteStageGroupUseCase interface {
	Execute(workspaceID, groupID string) error
}

type ListStageGroupsUseCase interface {
	Execute(workspaceID string, departmentIDs []string) ([]*StageGroup, error)
}

type GetStageGroupUseCase interface {
	Execute(workspaceID, groupID string) (*StageGroup, error)
}

// CloneStagesFromGroupUseCase copies the stages of a (workspace-owned) stage
// group onto a newly created campaign, the first item becomes the initial
// stage. Best-effort: it reports the first failure but always attempts every
// item, mirroring the original campaign-creation behavior.
type CloneStagesFromGroupUseCase interface {
	Execute(workspaceID, campaignID, campaignType, stageGroupID string) error
}

type CreateStageGroupInput struct {
	DepartmentID string                `json:"departmentId,omitempty"`
	Name         string                `json:"name"`
	Items        []StageGroupItemInput `json:"items"`
}

type UpdateStageGroupInput struct {
	Name  *string               `json:"name,omitempty"`
	Items []StageGroupItemInput `json:"items,omitempty"`
}

type StageGroupItemInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Position    int    `json:"position"`
}
