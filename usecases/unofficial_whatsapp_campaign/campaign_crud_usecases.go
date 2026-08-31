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

// recentEntriesLimit is how many rows the detail header previews.
const recentEntriesLimit = 20

// ---------------------------------------------------------------- update

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

	// Edits are refused while the campaign is RUNNING.
	//
	// The official campaign does not guard this, and that is a gap rather than a
	// precedent to follow: changing the message mid-blast means one list receives
	// two different texts with no record of who got which, and changing the
	// number mid-blast means half a campaign was sent from somewhere nobody
	// intended.
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
	// Copied from the input, which is what makes archiving persist: the handler
	// loads the campaign, flips Archived and calls Execute. Leaving it out drops
	// the flag silently — the row vanishes from the list optimistically and
	// reappears on reload, a bug the official channel shipped once already.
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

// ---------------------------------------------------------------- get

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

// ---------------------------------------------------------------- list

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

	// One aggregate query for the whole page, never a CountByStatus per row:
	// the official channel learned that the hard way as workspaces accumulated
	// campaigns and the list got slower with every one.
	ids := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.ID)
	}
	countsByCampaign, err := uc.repos.entries.CountByStatusForCampaigns(ids)
	if err != nil {
		return nil, err
	}
	for _, item := range result.Items {
		// NewMetrics tolerates a nil tally (a campaign with no entries is simply
		// absent from the map) and returns a zeroed object.
		item.Metrics = campaign.NewMetrics(countsByCampaign[item.ID])
	}

	enrichInstance(ctx, uc.instances, result.Items...)
	return result, nil
}

// ---------------------------------------------------------------- delete

type deleteCampaignUseCase struct{ repos campaignRepos }

func NewDeleteCampaignUseCase(campaigns uwc.Repository, entries uwc.EntryRepository) uwc.DeleteCampaignUseCase {
	return &deleteCampaignUseCase{repos: campaignRepos{campaigns: campaigns, entries: entries}}
}

func (uc *deleteCampaignUseCase) Execute(campaignID string) error {
	if campaignID == "" {
		return uwc.ErrCampaignNotFound
	}
	// Entries first: a campaign row removed while its entries survive leaves
	// orphans that every aggregate still counts.
	if err := uc.repos.entries.DeleteByCampaignID(campaignID); err != nil {
		return err
	}
	return uc.repos.campaigns.Delete(campaignID)
}

// ---------------------------------------------------------------- department

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

// ---------------------------------------------------------------- summary

type getSummaryUseCase struct{ aggregator uwc.SummaryAggregator }

func NewGetSummaryUseCase(aggregator uwc.SummaryAggregator) uwc.GetSummaryUseCase {
	return &getSummaryUseCase{aggregator: aggregator}
}

// Execute rolls entry statuses up across every campaign the filter selects.
//
// No balance-ledger branch and no by-category split, unlike the official
// summary: there is no ledger because there are no charges, and no categories
// because there are no templates. The tiles that would show them are already
// omitempty, so the same summary bar renders both channels.
func (uc *getSummaryUseCase) Execute(filter uwc.WorkspaceSummaryFilter) (*campaign.Metrics, error) {
	counts, err := uc.aggregator.CountByStatusForWorkspace(filter)
	if err != nil {
		return nil, err
	}
	return campaign.NewMetrics(counts), nil
}

// ---------------------------------------------------------------- entries list

type listEntriesUseCase struct{ entries uwc.EntryRepository }

func NewListEntriesUseCase(entries uwc.EntryRepository) uwc.ListEntriesUseCase {
	return &listEntriesUseCase{entries: entries}
}

func (uc *listEntriesUseCase) Execute(input uwc.ListEntriesInput) (*shared.PaginatedResult[*uwc.EntryWithLead], error) {
	return uc.entries.List(input)
}
