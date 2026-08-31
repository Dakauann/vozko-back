package unofficial_whatsapp_campaign

import (
	"fmt"
	"log"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// ---------------------------------------------------------------- reset

type resetCampaignUseCase struct{ repos campaignRepos }

func NewResetCampaignUseCase(campaigns uwc.Repository, entries uwc.EntryRepository) uwc.ResetCampaignUseCase {
	return &resetCampaignUseCase{repos: campaignRepos{campaigns: campaigns, entries: entries}}
}

func (uc *resetCampaignUseCase) PrepareReset(campaignID string) (*uwc.PrepareResetOutput, error) {
	if campaignID == "" {
		return nil, uwc.ErrCampaignNotFound
	}
	existing, err := uc.repos.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	// Resetting a live campaign would return entries to PENDING underneath a
	// consumer that is still sending them, producing duplicates.
	if existing.Status == campaign.StatusRunning {
		return nil, uwc.ErrCampaignResetNotAllowed
	}

	code, err := campaign.NewConfirmationCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate a reset code: %w", err)
	}
	if err := uc.repos.campaigns.UpdateResetCode(campaignID, code); err != nil {
		return nil, err
	}
	return &uwc.PrepareResetOutput{
		CampaignID: campaignID,
		ResetCode:  code,
		Message:    "Use this code to confirm the reset. Every number returns to PENDING and can be sent to again.",
	}, nil
}

func (uc *resetCampaignUseCase) ConfirmReset(in uwc.ResetCampaignInput) (*uwc.ResetCampaignOutput, error) {
	if in.CampaignID == "" {
		return nil, uwc.ErrCampaignNotFound
	}
	if in.ResetCode == "" {
		return nil, uwc.ErrCampaignResetCodeInvalid
	}

	existing, err := uc.repos.campaigns.FindByID(in.CampaignID)
	if err != nil {
		return nil, err
	}
	if existing.Status == campaign.StatusRunning {
		return nil, uwc.ErrCampaignResetNotAllowed
	}
	if existing.ResetCode == "" || existing.ResetCode != in.ResetCode {
		return nil, uwc.ErrCampaignResetCodeInvalid
	}

	resetCount, err := uc.repos.entries.ResetAllStatuses(in.CampaignID)
	if err != nil {
		return nil, fmt.Errorf("failed to reset entries: %w", err)
	}
	// The code is consumed, so a replayed confirmation cannot reset twice.
	if err := uc.repos.campaigns.UpdateResetCode(in.CampaignID, ""); err != nil {
		return nil, err
	}
	if _, err := uc.repos.campaigns.UpdateStatus(in.CampaignID, campaign.StatusStopped); err != nil {
		return nil, err
	}

	updated, err := uc.repos.campaigns.FindByID(in.CampaignID)
	if err != nil {
		return nil, err
	}
	if counts, err := uc.repos.entries.CountByStatus(in.CampaignID); err == nil {
		updated.Metrics = campaign.NewMetrics(counts)
	}

	newCode, _ := campaign.NewConfirmationCode()
	return &uwc.ResetCampaignOutput{
		Campaign:     updated,
		ResetCount:   resetCount,
		NewResetCode: newCode,
	}, nil
}

// ---------------------------------------------------------------- clear history

// ConversationWiper deletes the stored transcript for one conversation.
//
// Per-entry rather than a bulk method, because conversation.MessageRepository
// already offers exactly this and a new bulk variant would be a second way to
// do the same thing — with its own transaction semantics to get wrong.
type ConversationWiper interface {
	DeleteByEntry(entryID string, entryType shared.EntryType) error
}

type clearHistoryUseCase struct {
	repos campaignRepos
	wiper ConversationWiper
}

func NewClearHistoryUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	wiper ConversationWiper,
) uwc.ClearHistoryUseCase {
	return &clearHistoryUseCase{
		repos: campaignRepos{campaigns: campaigns, entries: entries},
		wiper: wiper,
	}
}

func (uc *clearHistoryUseCase) PrepareClearHistory(campaignID string) (*uwc.PrepareClearHistoryOutput, error) {
	existing, err := uc.repos.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	if existing.Status == campaign.StatusRunning {
		return nil, uwc.ErrCampaignClearNotAllowed
	}

	ids, err := uc.repos.entries.ConversationIDsForCampaign(campaignID)
	if err != nil {
		return nil, err
	}

	code, err := campaign.NewConfirmationCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate a clear code: %w", err)
	}
	if err := uc.repos.campaigns.UpdateClearCode(campaignID, code); err != nil {
		return nil, err
	}

	return &uwc.PrepareClearHistoryOutput{
		CampaignID:   campaignID,
		ClearCode:    code,
		MessageCount: int64(len(ids)),
		Message:      "Use this code to confirm. The stored conversations for this campaign are deleted permanently.",
	}, nil
}

func (uc *clearHistoryUseCase) ConfirmClearHistory(in uwc.ClearHistoryInput) (*uwc.ClearHistoryOutput, error) {
	existing, err := uc.repos.campaigns.FindByID(in.CampaignID)
	if err != nil {
		return nil, err
	}
	if existing.Status == campaign.StatusRunning {
		return nil, uwc.ErrCampaignClearNotAllowed
	}
	if existing.ClearCode == "" || existing.ClearCode != in.ClearCode {
		return nil, uwc.ErrCampaignClearCodeInvalid
	}

	ids, err := uc.repos.entries.ConversationIDsForCampaign(in.CampaignID)
	if err != nil {
		return nil, err
	}

	var deleted int64
	if uc.wiper != nil {
		for _, id := range ids {
			// One failure does not abort the wipe: a partially cleared campaign
			// is recoverable by running it again, while stopping halfway leaves
			// the operator with no way to tell what was removed.
			if err := uc.wiper.DeleteByEntry(id, shared.EntryTypeUnofficialWhatsApp); err != nil {
				log.Printf("[unofficial-whatsapp-campaign] could not clear conversation %s: %v", id, err)
				continue
			}
			deleted++
		}
	}

	if err := uc.repos.campaigns.UpdateClearCode(in.CampaignID, ""); err != nil {
		return nil, err
	}
	updated, err := uc.repos.campaigns.FindByID(in.CampaignID)
	if err != nil {
		return nil, err
	}

	newCode, _ := campaign.NewConfirmationCode()
	return &uwc.ClearHistoryOutput{
		Campaign:     updated,
		DeletedCount: deleted,
		NewClearCode: newCode,
	}, nil
}
