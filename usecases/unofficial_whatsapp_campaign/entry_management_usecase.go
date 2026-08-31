package unofficial_whatsapp_campaign

import (
	"context"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type entryManagementUseCase struct {
	repos campaignRepos
	leads LeadResolver
}

// The three entry use cases share one implementation and differ only in which
// Execute the port asks for. Thin adapters rather than three near-identical
// structs: the campaign lookup, the running-campaign guard and the lead bridge
// are the same code in all three, and three copies of them would drift.
type addEntriesAdapter struct{ *entryManagementUseCase }
type updateEntryAdapter struct{ *entryManagementUseCase }
type deleteEntryAdapter struct{ *entryManagementUseCase }

func (a addEntriesAdapter) Execute(ctx context.Context, in uwc.AddEntriesInput) (*uwc.AddEntriesOutput, error) {
	return a.add(ctx, in, false)
}

func (a updateEntryAdapter) Execute(ctx context.Context, in uwc.UpdateEntryInput) (*uwc.EntryOutput, error) {
	return a.updateEntry(ctx, in)
}

func (a deleteEntryAdapter) Execute(in uwc.DeleteEntryInput) error {
	return a.deleteEntry(in)
}

func newEntryManagement(campaigns uwc.Repository, entries uwc.EntryRepository, leads LeadResolver) *entryManagementUseCase {
	return &entryManagementUseCase{repos: campaignRepos{campaigns: campaigns, entries: entries}, leads: leads}
}

func NewAddEntriesUseCase(campaigns uwc.Repository, entries uwc.EntryRepository, leads LeadResolver) uwc.AddEntriesUseCase {
	return addEntriesAdapter{newEntryManagement(campaigns, entries, leads)}
}

func NewUpdateEntryUseCase(campaigns uwc.Repository, entries uwc.EntryRepository, leads LeadResolver) uwc.UpdateEntryUseCase {
	return updateEntryAdapter{newEntryManagement(campaigns, entries, leads)}
}

func NewDeleteEntryUseCase(campaigns uwc.Repository, entries uwc.EntryRepository) uwc.DeleteEntryUseCase {
	return deleteEntryAdapter{newEntryManagement(campaigns, entries, nil)}
}

// add appends numbers to an existing campaign.
//
// Invalid and duplicate rows are REPORTED rather than silently dropped: an
// operator pasting 500 numbers and being told "added 500" when 80 were malformed
// has no way to find the 80.
// allowRunning is false for the plain "add numbers" endpoint and true for quick
// send.
//
// The distinction is real rather than a bypass: adding to a running campaign
// without dispatching leaves rows PENDING forever, because the fan-out already
// happened and completion is counted per queued message. Quick send is the one
// caller that adds AND enqueues in the same breath, so it is the one caller for
// which adding to a live campaign is coherent.
func (uc *entryManagementUseCase) add(ctx context.Context, in uwc.AddEntriesInput, allowRunning bool) (*uwc.AddEntriesOutput, error) {
	camp, err := uc.repos.campaigns.FindByID(in.CampaignID)
	if err != nil {
		return nil, err
	}
	if !allowRunning && camp.Status == campaign.StatusRunning {
		return nil, uwc.ErrCampaignRunning
	}

	out := &uwc.AddEntriesOutput{}
	required := camp.Message.ParameterCount()

	seen := make(map[string]struct{}, len(in.Numbers))
	bulk := make([]lead.BulkLeadInput, 0, len(in.Numbers))
	valid := make([]uwc.EntryInput, 0, len(in.Numbers))

	for _, item := range in.Numbers {
		number := uw.NormalizePhone(item.Number)
		if !uwc.ValidTargetNumber(number) {
			out.InvalidSkipped++
			continue
		}
		if _, dup := seen[number]; dup {
			out.DuplicatesSkipped++
			continue
		}
		if len(item.Variables) < required {
			// A row without enough values would send a raw {{2}} to a customer.
			out.InvalidSkipped++
			continue
		}
		seen[number] = struct{}{}
		item.Number = number
		valid = append(valid, item)
		bulk = append(bulk, lead.BulkLeadInput{Number: number, Name: item.Name})
	}

	if len(valid) == 0 {
		return out, nil
	}

	leadsByNumber, err := uc.leads.FindOrCreateMany(camp.WorkspaceID, bulk)
	if err != nil {
		return nil, err
	}

	entries := make([]uwc.Entry, 0, len(valid))
	for _, item := range valid {
		l, ok := leadsByNumber[item.Number]
		if !ok || l == nil {
			out.InvalidSkipped++
			continue
		}
		entry := uwc.Entry{
			ID:          uuid.New().String(),
			CampaignID:  camp.ID,
			WorkspaceID: camp.WorkspaceID,
			LeadID:      l.ID,
			Number:      item.Number,
			Name:        item.Name,
			Status:      campaign.SendStatusPending,
			Variables:   item.Variables,
			Metadata:    item.Metadata,
		}
		entry.Normalize()
		entries = append(entries, entry)
	}

	created, err := uc.repos.entries.CreateMany(entries)
	if err != nil {
		return nil, err
	}

	// CreateMany skips rows the (campaign, lead) index already holds, so the
	// difference between what we offered and what came back IS the duplicate
	// count — reported rather than inferred.
	out.AddedCount = len(created)
	out.DuplicatesSkipped += len(entries) - len(created)
	for _, e := range created {
		out.Entries = append(out.Entries, entryOutput(e))
	}
	return out, nil
}

func (uc *entryManagementUseCase) deleteEntry(in uwc.DeleteEntryInput) error {
	entry, err := uc.repos.entries.FindByID(in.EntryID)
	if err != nil {
		return err
	}
	// The campaign id in the path has to match the entry's, or a caller could
	// delete another campaign's row by guessing an id.
	if entry.CampaignID != in.CampaignID {
		return uwc.ErrEntryNotFound
	}
	return uc.repos.entries.Delete(in.EntryID)
}

func (uc *entryManagementUseCase) updateEntry(ctx context.Context, in uwc.UpdateEntryInput) (*uwc.EntryOutput, error) {
	entry, err := uc.repos.entries.FindByID(in.EntryID)
	if err != nil {
		return nil, err
	}
	if entry.CampaignID != in.CampaignID {
		return nil, uwc.ErrEntryNotFound
	}

	camp, err := uc.repos.campaigns.FindByID(in.CampaignID)
	if err != nil {
		return nil, err
	}
	if camp.Status == campaign.StatusRunning {
		return nil, uwc.ErrCampaignRunning
	}

	if in.Number != nil {
		number := uw.NormalizePhone(*in.Number)
		if !uwc.ValidTargetNumber(number) {
			return nil, uwc.ErrCampaignTargetInvalid
		}
		// A changed number is a different person, so the lead has to move with
		// it — otherwise the entry would point at the previous lead's history.
		leads, err := uc.leads.FindOrCreateMany(camp.WorkspaceID, []lead.BulkLeadInput{{Number: number}})
		if err != nil {
			return nil, err
		}
		if l, ok := leads[number]; ok && l != nil {
			entry.LeadID = l.ID
		}
		entry.Number = number
	}
	if in.Name != nil {
		entry.Name = *in.Name
	}
	if in.Variables != nil {
		entry.Variables = in.Variables
	}
	if in.Metadata != nil {
		entry.Metadata = in.Metadata
	}
	entry.Normalize()

	if err := uc.repos.entries.UpdateEntryDetails(entry.ID, uwc.UpdateEntryDetails{
		LeadID:    entry.LeadID,
		Number:    entry.Number,
		Name:      entry.Name,
		Variables: entry.Variables,
		Metadata:  entry.Metadata,
	}); err != nil {
		return nil, err
	}

	saved, err := uc.repos.entries.FindByID(in.EntryID)
	if err != nil {
		return nil, err
	}
	out := entryOutput(*saved)
	return &out, nil
}

func entryOutput(e uwc.Entry) uwc.EntryOutput {
	return uwc.EntryOutput{
		EntryID:        e.ID,
		CampaignID:     e.CampaignID,
		LeadID:         e.LeadID,
		Number:         e.Number,
		Name:           e.Name,
		Variables:      e.Variables,
		Status:         string(e.Status),
		ConversationID: e.ConversationID,
		Metadata:       e.Metadata,
		CreatedAt:      e.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      e.UpdatedAt.Format(time.RFC3339),
	}
}
