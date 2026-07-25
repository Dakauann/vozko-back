package stage

type Repository interface {
	Create(tag *Stage) error
	Update(tag *Stage) error
	Delete(id string) error
	FindByID(id string) (*Stage, error)
	FindIDsByName(workspaceID, name string) ([]string, error)
	ListByWorkspace(workspaceID string) ([]*Stage, error)
	ListDistinctByWorkspace(workspaceID string) ([]*Stage, error)
	ListByCampaign(workspaceID, campaignID, campaignType string) ([]*Stage, error)
	ListByCampaignIDs(workspaceID string, campaignIDs []string) (map[string][]*Stage, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*Stage, error)

	// EnsureDefaultOpportunityPipeline returns (creating + seeding if absent) the
	// workspace's default sales pipeline id, so the opportunity board works out of
	// the box. Idempotent; mirrors the conversation pipeline self-heal.
	EnsureDefaultOpportunityPipeline(workspaceID string) (string, error)

	// CreateConversationPipeline creates a new (non-default) conversation pipeline
	// for a campaign's own funnel and returns its id. stageGroupID records the group
	// it was stamped from (empty for a direct/manual funnel) so the same group reuses
	// one pipeline. SetCampaignPipeline points a campaign at a pipeline. Together they
	// give a campaign a distinct funnel (pipeline-scoped stages) instead of orphan
	// campaign_id clones.
	CreateConversationPipeline(workspaceID, name, stageGroupID string) (string, error)
	// FindConversationPipelineByGroup returns the id of the workspace's conversation
	// pipeline stamped from stageGroupID, or "" if none exists yet.
	FindConversationPipelineByGroup(workspaceID, stageGroupID string) (string, error)
	SetCampaignPipeline(campaignID, campaignType, pipelineID string) error
	NameExistsInCampaign(workspaceID, campaignID, campaignType, name string, excludeID *string) (bool, error)

	SetInitialStage(workspaceID, campaignID, campaignType, StageID string) error
	ClearInitialStage(workspaceID string) error
	GetInitialStage(workspaceID string) (*Stage, error)
	GetInitialStageForCampaign(workspaceID, campaignID, campaignType string) (*Stage, error)

	ReorderStages(workspaceID string, tagIDs []string) error

	AssignStage(EntryStage *EntryStage) error
	RemoveStage(StageID, entryID, entryType, workspaceID string) error
	RemoveEntryStage(entryID, entryType, workspaceID string) error
	GetEntryStage(entryID, entryType, workspaceID string) (*EntryStage, error)
	GetBatchEntryStages(entryIDs []string, entryType, workspaceID string) (map[string]*EntryStage, error)
	GetEntriesByStage(StageID, workspaceID string) ([]*EntryStage, error)

	GetStageCountsForCampaign(workspaceID, campaignID, entryType string) (map[string]int64, error)
	GetStageCountsForWorkspace(workspaceID, entryType string) (map[string]int64, error)

	EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType string) error

	DeduplicateStages(workspaceID string) error
}

type StageGroupRepository interface {
	Create(group *StageGroup) error
	Update(group *StageGroup) error
	Delete(id string) error
	FindByID(id string) (*StageGroup, error)
	ListByWorkspace(workspaceID string) ([]*StageGroup, error)
	ListByWorkspaceAndDepartments(workspaceID string, departmentIDs []string) ([]*StageGroup, error)
	AddItem(item *StageGroupItem) error
	RemoveItem(itemID string) error
	GetItems(StageGroupID string) ([]StageGroupItem, error)
}
