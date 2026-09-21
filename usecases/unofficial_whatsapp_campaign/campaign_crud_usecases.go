package unofficial_whatsapp_campaign

import (
	"context"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	workspace_department "vozko/domain/workspace/workspace_department"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

const recentEntriesLimit = 20

type updateCampaignUseCase struct {
	repos     campaignRepos
	instances InstanceGateway
}

func NewUpdateCampaignUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	instances InstanceGateway,
) uwc.UpdateCampaignUseCase {
	return &updateCampaignUseCase{
		repos:     campaignRepos{campaigns: campaigns, entries: entries},
		instances: instances,
	}
}

func (uc *updateCampaignUseCase) Execute(
	ctx context.Context,
	campaignID string,
	in *uwc.Campaign,
	scope uw.DepartmentScope,
) (*uwc.Campaign, error) {
	if campaignID == "" {
		return nil, uwc.ErrCampaignNotFound
	}
	if in == nil {
		return nil, uwc.ErrCampaignNameRequired
	}

	existing, err := uc.repos.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}

	if existing.Status == campaign.StatusRunning {
		return nil, uwc.ErrCampaignRunning
	}

	existing.Name = in.Name
	existing.Message = in.Message
	existing.AgentID = in.AgentID
	existing.WorkflowID = in.WorkflowID
	existing.PipelineID = in.PipelineID
	existing.EnableAgentResponses = in.EnableAgentResponses
	existing.EnableWorkflow = in.EnableWorkflow
	existing.EnableAnalysis = in.EnableAnalysis
	existing.EnableAutoStaging = in.EnableAutoStaging
	existing.EnableAutoMemory = in.EnableAutoMemory
	existing.PreferAudio = in.PreferAudio
	existing.AiModel = in.AiModel
	existing.SendDelayMinMS = in.SendDelayMinMS
	existing.SendDelayMaxMS = in.SendDelayMaxMS
	existing.DailyCap = in.DailyCap
	existing.Archived = in.Archived

	if in.InstanceID != "" {
		existing.InstanceID = in.InstanceID
	}

	existing.Normalize()
	if err := existing.ValidateMetadata(); err != nil {
		return nil, err
	}

	instance, err := uc.instances.Instance(ctx, existing.InstanceID)
	if err != nil {
		return nil, err
	}
	if err := uwuc.EnsureVisible(instance, existing.WorkspaceID, scope); err != nil {
		return nil, err
	}
	if err := ensureInstanceCanCampaign(instance); err != nil {
		return nil, err
	}

	if err := uc.repos.campaigns.Update(campaignID, existing); err != nil {
		return nil, err
	}

	saved, err := uc.repos.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	if counts, err := uc.repos.entries.CountByStatus(campaignID); err == nil {
		saved.Metrics = campaign.NewMetrics(counts)
	}
	enrichInstance(ctx, uc.instances, saved)
	return saved, nil
}

type getCampaignUseCase struct {
	repos     campaignRepos
	instances InstanceGateway
}

func NewGetCampaignUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	instances InstanceGateway,
) uwc.GetCampaignUseCase {
	return &getCampaignUseCase{
		repos:     campaignRepos{campaigns: campaigns, entries: entries},
		instances: instances,
	}
}

func (uc *getCampaignUseCase) Execute(ctx context.Context, campaignID string) (*uwc.Campaign, error) {
	c, err := uc.repos.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	if counts, err := uc.repos.entries.CountByStatus(campaignID); err == nil {
		c.Metrics = campaign.NewMetrics(counts)
	}
	if recent, err := uc.repos.entries.ListRecentlyUpdated(campaignID, recentEntriesLimit); err == nil {
		c.RecentEntries = recent
	}
	enrichInstance(ctx, uc.instances, c)
	return c, nil
}

type listCampaignsUseCase struct {
	repos     campaignRepos
	instances InstanceGateway
}

func NewListCampaignsUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	instances InstanceGateway,
) uwc.ListCampaignsUseCase {
	return &listCampaignsUseCase{
		repos:     campaignRepos{campaigns: campaigns, entries: entries},
		instances: instances,
	}
}

func (uc *listCampaignsUseCase) Execute(
	ctx context.Context,
	input uwc.ListCampaignsInput,
) (*shared.PaginatedResult[*uwc.Campaign], error) {
	result, err := uc.repos.campaigns.List(input)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Items) == 0 {
		return result, nil
	}

	ids := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.ID)
	}
	countsByCampaign, err := uc.repos.entries.CountByStatusForCampaigns(ids)
	if err != nil {
		return nil, err
	}
	for _, item := range result.Items {
		item.Metrics = campaign.NewMetrics(countsByCampaign[item.ID])
	}

	enrichInstance(ctx, uc.instances, result.Items...)
	return result, nil
}

type deleteCampaignUseCase struct{ repos campaignRepos }

func NewDeleteCampaignUseCase(campaigns uwc.Repository, entries uwc.EntryRepository) uwc.DeleteCampaignUseCase {
	return &deleteCampaignUseCase{repos: campaignRepos{campaigns: campaigns, entries: entries}}
}

func (uc *deleteCampaignUseCase) Execute(campaignID string) error {
	if campaignID == "" {
		return uwc.ErrCampaignNotFound
	}
	if err := uc.repos.entries.DeleteByCampaignID(campaignID); err != nil {
		return err
	}
	return uc.repos.campaigns.Delete(campaignID)
}

type assignDepartmentUseCase struct {
	campaigns uwc.Repository
	resolver  workspace_department.CreationDepartmentResolver
}

func NewAssignDepartmentUseCase(
	campaigns uwc.Repository,
	resolver workspace_department.CreationDepartmentResolver,
) uwc.AssignDepartmentUseCase {
	return &assignDepartmentUseCase{campaigns: campaigns, resolver: resolver}
}

func (uc *assignDepartmentUseCase) Execute(ctx context.Context, campaignID string) (*uwc.Campaign, error) {
	existing, err := uc.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	if uc.resolver == nil {
		return existing, nil
	}
	departmentID, err := uc.resolver.Resolve(ctx, existing.WorkspaceID)
	if err != nil {
		return nil, err
	}
	existing.DepartmentID = departmentID
	if err := uc.campaigns.Update(campaignID, existing); err != nil {
		return nil, err
	}
	return uc.campaigns.FindByID(campaignID)
}

type getSummaryUseCase struct{ aggregator uwc.SummaryAggregator }

func NewGetSummaryUseCase(aggregator uwc.SummaryAggregator) uwc.GetSummaryUseCase {
	return &getSummaryUseCase{aggregator: aggregator}
}

func (uc *getSummaryUseCase) Execute(filter uwc.WorkspaceSummaryFilter) (*campaign.Metrics, error) {
	counts, err := uc.aggregator.CountByStatusForWorkspace(filter)
	if err != nil {
		return nil, err
	}
	return campaign.NewMetrics(counts), nil
}

type listEntriesUseCase struct{ entries uwc.EntryRepository }

func NewListEntriesUseCase(entries uwc.EntryRepository) uwc.ListEntriesUseCase {
	return &listEntriesUseCase{entries: entries}
}

func (uc *listEntriesUseCase) Execute(input uwc.ListEntriesInput) (*shared.PaginatedResult[*uwc.EntryWithLead], error) {
	return uc.entries.List(input)
}
