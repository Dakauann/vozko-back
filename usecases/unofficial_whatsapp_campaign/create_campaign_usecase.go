package unofficial_whatsapp_campaign

import (
	"context"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	workspace_department "vozko/domain/workspace/workspace_department"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type createCampaignUseCase struct {
	repos              campaignRepos
	leads              LeadResolver
	instances          InstanceGateway
	spam               SpamGuard
	departmentResolver workspace_department.CreationDepartmentResolver
}

func NewCreateCampaignUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	leads LeadResolver,
	instances InstanceGateway,
	spam SpamGuard,
	departmentResolver workspace_department.CreationDepartmentResolver,
) uwc.CreateCampaignUseCase {
	return &createCampaignUseCase{
		repos:              campaignRepos{campaigns: campaigns, entries: entries},
		leads:              leads,
		instances:          instances,
		spam:               spam,
		departmentResolver: departmentResolver,
	}
}

func (uc *createCampaignUseCase) Execute(
	ctx context.Context,
	in *uwc.Campaign,
	scope uw.DepartmentScope,
) (*uwc.Campaign, error) {
	if in == nil {
		return nil, uwc.ErrCampaignNameRequired
	}

	// Whether the CALLER specified pacing has to be read before Normalize, which
	// fills in the channel defaults. Reading it afterwards can never see an
	// unset value, so the number's own range would never be copied.
	pacingUnset := in.SendDelayMinMS <= 0 && in.SendDelayMaxMS <= 0

	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}

	instance, err := uc.instances.Instance(ctx, in.InstanceID)
	if err != nil {
		return nil, err
	}
	// Ownership is checked here as well as by the route's permission gate: the
	// gate proves the caller may run campaigns SOMEWHERE in their workspace, not
	// from this particular number.
	if err := uwuc.EnsureVisible(instance, in.WorkspaceID, scope); err != nil {
		return nil, err
	}
	if err := ensureInstanceCanCampaign(instance); err != nil {
		return nil, err
	}

	if uc.departmentResolver != nil {
		departmentID, err := uc.departmentResolver.Resolve(ctx, in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		in.DepartmentID = departmentID
	}

	// Pacing defaults are COPIED from the number, then re-clamped by Normalize.
	// Copying rather than referencing is what stops a later widening of the
	// number's range from silently speeding up a blast already in flight.
	if pacingUnset {
		in.SendDelayMinMS, in.SendDelayMaxMS = instance.SendDelayRange()
		in.Normalize()
	}

	if err := in.ValidateTargetVariables(in.Message.ParameterCount()); err != nil {
		return nil, err
	}

	if in.ID == "" {
		in.ID = uuid.New().String()
	}
	if err := uc.repos.campaigns.Create(in); err != nil {
		return nil, err
	}

	if err := uc.materializeTargets(ctx, in, instance); err != nil {
		return nil, err
	}

	return uc.hydrate(ctx, in.ID)
}

// materializeTargets turns the imported list into lead-backed entries.
//
// The lead bridge is the SAME FindOrCreateMany the official campaign uses, which
// is what makes these contacts reachable by exports, boletos and every other
// lead-keyed tool in the CRM rather than living in a silo.
func (uc *createCampaignUseCase) materializeTargets(
	ctx context.Context,
	in *uwc.Campaign,
	instance *uw.Instance,
) error {
	bulk := make([]lead.BulkLeadInput, 0, len(in.Targets))
	for _, t := range in.Targets {
		bulk = append(bulk, lead.BulkLeadInput{Number: t.Number, Name: t.Name})
	}

	leadsByNumber, err := uc.leads.FindOrCreateMany(in.WorkspaceID, bulk)
	if err != nil {
		return err
	}

	entries := make([]uwc.Entry, 0, len(in.Targets))
	seen := make(map[string]struct{}, len(in.Targets))
	for _, t := range in.Targets {
		l, found := leadsByNumber[t.Number]
		if !found || l == nil {
			continue
		}
		// The (campaign, lead) index refuses duplicates anyway; skipping here
		// keeps the reported count honest rather than claiming rows the database
		// silently dropped.
		if _, dup := seen[l.ID]; dup {
			continue
		}
		seen[l.ID] = struct{}{}

		entry := uwc.Entry{
			ID:          uuid.New().String(),
			CampaignID:  in.ID,
			WorkspaceID: in.WorkspaceID,
			LeadID:      l.ID,
			Number:      t.Number,
			Name:        t.Name,
			Status:      campaign.SendStatusPending,
			Variables:   t.Variables,
			Metadata:    t.Metadata,
		}
		entry.Normalize()
		entries = append(entries, entry)
	}

	created, err := uc.repos.entries.CreateMany(entries)
	if err != nil {
		return err
	}

	uc.markSpamEntries(ctx, created, in.WorkspaceID, instance.ID)
	return nil
}

// markSpamEntries pre-marks the recipients the workspace's own cooldown will
// refuse.
//
// Done at import rather than only at send time so an operator sees the real
// reachable count BEFORE they start — a campaign that silently sends to 400 of
// its 1.000 numbers is one nobody can plan around.
func (uc *createCampaignUseCase) markSpamEntries(
	ctx context.Context,
	entries []uwc.Entry,
	workspaceID, instanceID string,
) {
	if uc.spam == nil || len(entries) == 0 {
		return
	}
	leadIDs := make([]string, 0, len(entries))
	for _, e := range entries {
		leadIDs = append(leadIDs, e.LeadID)
	}

	skip := uc.spam.SkipMany(ctx, workspaceID, leadIDs, instanceID)
	for _, e := range entries {
		if skip[e.LeadID] {
			_ = uc.repos.entries.UpdateStatus(
				e.ID, campaign.SendStatusNotEligiblePossibleSpam, "", 0, "")
		}
	}
}

func (uc *createCampaignUseCase) hydrate(ctx context.Context, campaignID string) (*uwc.Campaign, error) {
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

// ensureInstanceCanCampaign refuses a number a campaign could only fail on.
//
// A banned number is terminal: no scan recovers it, so offering to campaign from
// one is offering something that can only produce 150.000 failures.
func ensureInstanceCanCampaign(instance *uw.Instance) error {
	switch instance.Status {
	case uw.StatusBanned:
		return uwc.NewInstanceUnusableError(instance.Label(),
			"WhatsApp disabled this number; it cannot be recovered")
	case uw.StatusProvisionFailed:
		return uwc.NewInstanceUnusableError(instance.Label(),
			"this number was never finished connecting")
	}
	return nil
}

// enrichInstance attaches the number's label and live session state.
//
// List enrichment, not stored on the campaign — the same idiom as TemplateName
// on the official channel. The screen needs to show which number a campaign
// belongs to and whether it can send, without a request per row.
func enrichInstance(ctx context.Context, gateway InstanceGateway, campaigns ...*uwc.Campaign) {
	if gateway == nil {
		return
	}
	cache := map[string]*uw.Instance{}
	for _, c := range campaigns {
		if c == nil || c.InstanceID == "" {
			continue
		}
		instance, ok := cache[c.InstanceID]
		if !ok {
			var err error
			instance, err = gateway.Instance(ctx, c.InstanceID)
			if err != nil || instance == nil {
				continue
			}
			cache[c.InstanceID] = instance
		}
		c.InstanceLabel = instance.Label()
		c.InstanceStatus = string(instance.Status)
		c.InstanceSessionLive = instance.SessionLive()
	}
}
