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

type ListStagesUseCase interface {
	Execute(workspaceID, campaignID, campaignType string) ([]*Stage, error)
}

type SetInitialStageUseCase interface {
	Execute(workspaceID, StageID string) (*Stage, error)
}

type AssignEntryStageUseCase interface {
	Execute(workspaceID string, input AssignEntryStageInput) (*EntryStage, error)
}

type RemoveEntryStageUseCase interface {
	Execute(workspaceID, StageID, entryID, entryType string) error
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
}

type UpdateStageInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Color       *string `json:"color,omitempty"`
}

type AssignEntryStageInput struct {
	StageID   string `json:"StageID"`
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
}

type ReorderStagesInput struct {
	StageIDs     []string `json:"stageIds"`
	CampaignID   string   `json:"campaignId"`
	CampaignType string   `json:"campaignType"`
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
