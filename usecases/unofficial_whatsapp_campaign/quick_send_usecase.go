package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// ErrQuickSendBusy means another quick send is already in flight.
var ErrQuickSendBusy = errors.New("unofficial whatsapp campaign quick send: busy, please retry")

const quickSendLockTTL = 30 * time.Second

// quickSendUseCase adds numbers to a live campaign and dispatches just those.
//
// The one path that is allowed to enqueue work onto a RUNNING campaign, which is
// why it holds a lock: two concurrent quick sends would each fan out and each
// overwrite the completion counter, leaving a campaign that finishes early and
// abandons whatever the loser queued.
type quickSendUseCase struct {
	repos    campaignRepos
	leads    LeadResolver
	dispatch uwc.DispatchCampaignUseCase
	shared   cache.SharedState
}

func NewQuickSendUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	leads LeadResolver,
	dispatch uwc.DispatchCampaignUseCase,
	sharedState cache.SharedState,
) uwc.QuickSendUseCase {
	return &quickSendUseCase{
		repos:    campaignRepos{campaigns: campaigns, entries: entries},
		leads:    leads,
		dispatch: dispatch,
		shared:   sharedState,
	}
}

func (uc *quickSendUseCase) Execute(ctx context.Context, in uwc.QuickSendInput) (*uwc.QuickSendOutput, error) {
	if in.CampaignID == "" {
		return nil, uwc.ErrCampaignNotFound
	}

	if uc.shared != nil {
		key := uwc.QueueNamespace.QuickSendLockKey(in.CampaignID)
		acquired, err := uc.shared.SetNX(key, "1", quickSendLockTTL)
		if err != nil {
			return nil, fmt.Errorf("unofficial whatsapp campaign quick send: could not take the lock: %w", err)
		}
		if !acquired {
			return nil, ErrQuickSendBusy
		}
		defer func() { _ = uc.shared.Del(key) }()
	}

	camp, err := uc.repos.campaigns.FindByID(in.CampaignID)
	if err != nil {
		return nil, err
	}

	out := &uwc.QuickSendOutput{CampaignID: in.CampaignID}

	// New numbers are added first, while the campaign may still be stopped, so
	// the add path's own running-campaign guard does not refuse them.
	var added []uwc.EntryOutput
	if len(in.Numbers) > 0 {
		result, err := uc.addNumbers(ctx, camp, in.Numbers)
		if err != nil {
			return nil, err
		}
		out.AddedCount = result.AddedCount
		out.DuplicatesSkipped = result.DuplicatesSkipped
		added = result.Entries
		out.Entries = added
	}

	// Dispatch only what this call is responsible for. Handing an empty entry
	// list to Dispatch would fan out every pending row in the campaign, turning
	// "send to these three" into "send to all forty thousand".
	entries := make([]uwc.DispatchEntry, 0, len(added))
	for _, e := range added {
		entries = append(entries, uwc.DispatchEntry{EntryID: e.EntryID, PhoneNumber: e.Number})
	}
	if len(entries) == 0 {
		pending, err := uc.repos.entries.ListByStatus(
			in.CampaignID, campaign.SendStatusPending, maxPendingFanOut)
		if err != nil {
			return nil, err
		}
		for _, e := range pending {
			entries = append(entries, uwc.DispatchEntry{EntryID: e.ID, PhoneNumber: e.Number})
		}
	}

	if err := uc.dispatch.Dispatch(ctx, uwc.DispatchCampaignInput{
		CampaignID: in.CampaignID,
		Entries:    entries,
		Action:     campaign.ActionStart,
	}); err != nil && !errors.Is(err, campaign.ErrAlreadyRunning) {
		return nil, err
	}

	out.DispatchedCount = len(entries)
	if refreshed, err := uc.repos.campaigns.FindByID(in.CampaignID); err == nil {
		out.Status = string(refreshed.Status)
	}
	return out, nil
}

// addNumbers reuses the add-entries path rather than repeating its validation,
// lead bridging and duplicate accounting.
func (uc *quickSendUseCase) addNumbers(
	ctx context.Context,
	camp *uwc.Campaign,
	numbers []uwc.EntryInput,
) (*uwc.AddEntriesOutput, error) {
	adder := newEntryManagement(uc.repos.campaigns, uc.repos.entries, uc.leads)
	return adder.add(ctx, uwc.AddEntriesInput{CampaignID: camp.ID, Numbers: numbers}, true)
}
