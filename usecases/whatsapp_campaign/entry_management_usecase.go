package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type deleteEntryUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
}

func NewDeleteEntryUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
) wc.DeleteEntryUseCase {
	return &deleteEntryUseCase{
		campaignRepo: campaignRepo,
		entryRepo:    entryRepo,
	}
}

func (uc *deleteEntryUseCase) Execute(input wc.DeleteEntryInput) error {
	c, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return wc.ErrCampaignNotFound
	}

	if c.Status == wc.CampaignStatusRunning {
		return wc.ErrCampaignRunning
	}

	entry, err := uc.entryRepo.FindByID(input.EntryID)
	if err != nil || entry == nil {
		return wc.ErrEntryNotFound
	}
	if entry.CampaignID != input.CampaignID {
		return wc.ErrEntryNotFound
	}

	return uc.entryRepo.Delete(input.EntryID)
}

type AIToggleTelemetrySink interface {
	AIToggle(workspaceID, entryID, entryType, actorUserID string, enabled bool)
}

var errLeadMergerMissing = errors.New("whatsapp campaign entry: the lead merge is not wired, a name edit cannot reach the lead")

type LeadMerger interface {
	MergeIncoming(ctx context.Context, workspaceID, leadID string, update lead.LeadUpdate) (*lead.Lead, error)
}

type updateEntryUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
	leadRepo     lead.Repository
	leadMerger   LeadMerger
	aiToggle     AIToggleTelemetrySink
}

func NewUpdateEntryUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	leadRepo lead.Repository,
	leadMerger LeadMerger,
) *updateEntryUseCase {
	return &updateEntryUseCase{
		campaignRepo: campaignRepo,
		entryRepo:    entryRepo,
		leadRepo:     leadRepo,
		leadMerger:   leadMerger,
	}
}

func (uc *updateEntryUseCase) SetAIToggleTelemetry(t AIToggleTelemetrySink) {
	if uc != nil {
		uc.aiToggle = t
	}
}

func (uc *updateEntryUseCase) Execute(input wc.UpdateEntryInput) (*wc.EntryOutput, error) {
	c, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, wc.ErrCampaignNotFound
	}

	isOnlyAIToggle := input.AutomationEnabled != nil && !input.ChangesTheRecipient()
	if c.Status == wc.CampaignStatusRunning && !isOnlyAIToggle {
		return nil, wc.ErrCampaignRunning
	}
	if err := campaign.RefuseSelectionChange(c.Source, input.ChangesTheRecipient()); err != nil {
		return nil, err
	}

	entry, err := uc.entryRepo.FindByID(input.EntryID)
	if err != nil || entry == nil {
		return nil, wc.ErrEntryNotFound
	}
	if entry.CampaignID != input.CampaignID {
		return nil, wc.ErrEntryNotFound
	}

	currentLead, err := uc.leadRepo.FindByID(c.WorkspaceID, entry.LeadID)
	if err != nil {
		return nil, err
	}

	if len(input.Variables) > 0 {
		entry.Variables = input.Variables
	}

	if input.Number != nil && *input.Number != "" {
		normalizedNumber := lead.NormalizeNumber(*input.Number)
		if normalizedNumber == "" {
			return nil, wc.ErrCampaignPhoneNumberInvalid
		}

		if normalizedNumber != currentLead.Number {
			existingEntry, _ := uc.entryRepo.FindByCampaignAndNumber(input.CampaignID, normalizedNumber)
			if existingEntry != nil && existingEntry.ID != input.EntryID {
				return nil, wc.ErrEntryDuplicate
			}

			leadUpdate := lead.LeadUpdate{Source: lead.SourceImport, Name: currentLead.Name}
			if input.Name != nil {
				leadUpdate.Name = *input.Name
			}

			newLead, _, err := uc.leadRepo.FindOrCreate(c.WorkspaceID, normalizedNumber, leadUpdate)
			if err != nil {
				return nil, err
			}

			entryMetadata := entry.Metadata
			if input.Metadata != nil {
				entryMetadata = input.Metadata
			}

			variables := entry.Variables
			if len(input.Variables) > 0 {
				variables = input.Variables
			}
			newEntry := &wce.WhatsAppCampaignEntry{
				ID:         uuid.New().String(),
				CampaignID: input.CampaignID,
				LeadID:     newLead.ID,
				Status:     wce.SendStatusPending,
				Variables:  variables,
				Metadata:   entryMetadata,
			}
			if err := uc.entryRepo.Create(newEntry); err != nil {
				return nil, err
			}

			if err := uc.entryRepo.Delete(input.EntryID); err != nil {
				return nil, err
			}

			return &wc.EntryOutput{
				EntryID:           newEntry.ID,
				CampaignID:        newEntry.CampaignID,
				LeadID:            newLead.ID,
				Number:            newLead.Number,
				Name:              newLead.Name,
				Variables:         newEntry.Variables,
				Status:            string(newEntry.Status),
				AutomationEnabled: newEntry.AutomationEnabled,
				Metadata:          entryMetadata,
				CreatedAt:         newEntry.CreatedAt.Format(time.RFC3339),
				UpdatedAt:         newEntry.UpdatedAt.Format(time.RFC3339),
			}, nil
		}
	}

	if input.Name != nil {
		if err := uc.mergeEntryName(c.WorkspaceID, currentLead, *input.Name); err != nil {
			return nil, err
		}
	}

	if input.Metadata != nil {
		if err := uc.entryRepo.UpdateMetadata(input.EntryID, input.Metadata); err != nil {
			return nil, err
		}
	}

	if len(input.Variables) > 0 {
		entry.Variables = input.Variables
		if err := uc.entryRepo.UpsertCampaignEntries(input.CampaignID, []wce.WhatsAppCampaignEntry{*entry}); err != nil {
			return nil, err
		}
	}

	if input.AutomationEnabled != nil {
		if err := uc.entryRepo.UpdateAutomationEnabled(input.EntryID, input.AutomationEnabled); err != nil {
			return nil, err
		}
		if uc.aiToggle != nil {
			uc.aiToggle.AIToggle(c.WorkspaceID, input.EntryID, "whatsapp", "", *input.AutomationEnabled)
		}
	}

	updatedLead, err := uc.leadRepo.FindByID(c.WorkspaceID, currentLead.ID)
	if err != nil {
		return nil, err
	}

	updatedEntry, err := uc.entryRepo.FindByID(input.EntryID)
	if err != nil {
		return nil, err
	}

	return &wc.EntryOutput{
		EntryID:           updatedEntry.ID,
		CampaignID:        updatedEntry.CampaignID,
		LeadID:            updatedLead.ID,
		Number:            updatedLead.Number,
		Name:              updatedLead.Name,
		Variables:         updatedEntry.Variables,
		Status:            string(updatedEntry.Status),
		AutomationEnabled: updatedEntry.AutomationEnabled,
		Metadata:          updatedEntry.Metadata,
		CreatedAt:         updatedEntry.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         updatedEntry.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (uc *updateEntryUseCase) mergeEntryName(workspaceID string, current *lead.Lead, name string) error {
	if uc.leadMerger == nil {
		return errLeadMergerMissing
	}
	_, err := uc.leadMerger.MergeIncoming(context.Background(), workspaceID, current.ID, lead.LeadUpdate{Source: lead.SourceImport, Name: name})
	return err
}

type addEntriesUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
	leadRepo     lead.Repository
	automation   AutomationCheck
}

func (uc *addEntriesUseCase) SetAutomation(automation AutomationCheck) {
	uc.automation = automation
}

func NewAddEntriesUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	leadRepo lead.Repository,
) wc.AddEntriesUseCase {
	return &addEntriesUseCase{
		campaignRepo: campaignRepo,
		entryRepo:    entryRepo,
		leadRepo:     leadRepo,
	}
}

func (uc *addEntriesUseCase) Execute(input wc.AddEntriesInput) (*wc.AddEntriesOutput, error) {
	c, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, wc.ErrCampaignNotFound
	}

	if c.Status == wc.CampaignStatusRunning {
		return nil, wc.ErrCampaignRunning
	}

	if len(input.PhoneNumbers) == 0 {
		return nil, wc.ErrCampaignPhoneNumbersRequired
	}

	if err := guardCampaignChange(c, uc.automation, true, entriesMetadata(input.PhoneNumbers)); err != nil {
		return nil, err
	}

	currentCount, err := uc.entryRepo.CountByCampaignID(input.CampaignID)
	if err != nil {
		return nil, err
	}
	if int(currentCount)+len(input.PhoneNumbers) > wc.MaxCampaignPhoneNumbers {
		return nil, wc.ErrCampaignPhoneNumbersTooMany
	}

	for _, phoneInput := range input.PhoneNumbers {
		normalized := lead.NormalizeNumber(phoneInput.Number)
		if normalized == "" {
			return nil, wc.ErrCampaignPhoneNumberInvalid
		}
	}

	output := &wc.AddEntriesOutput{
		Entries: make([]wc.EntryOutput, 0),
	}

	for _, phoneInput := range input.PhoneNumbers {
		normalizedNumber := lead.NormalizeNumber(phoneInput.Number)

		existingEntry, _ := uc.entryRepo.FindByCampaignAndNumber(input.CampaignID, normalizedNumber)
		if existingEntry != nil {
			output.DuplicatesSkipped++
			continue
		}

		leadUpdate := lead.LeadUpdate{
			Source: lead.SourceImport,
			Name:   phoneInput.Name,
		}
		l, _, err := uc.leadRepo.FindOrCreate(c.WorkspaceID, normalizedNumber, leadUpdate)
		if err != nil {
			return nil, err
		}

		entry := &wce.WhatsAppCampaignEntry{
			ID:         uuid.New().String(),
			CampaignID: input.CampaignID,
			LeadID:     l.ID,
			Status:     wce.SendStatusPending,
			Variables:  phoneInput.Variables,
			Metadata:   phoneInput.Metadata,
		}
		if err := uc.entryRepo.Create(entry); err != nil {
			return nil, err
		}

		output.AddedCount++
		output.Entries = append(output.Entries, wc.EntryOutput{
			EntryID:    entry.ID,
			CampaignID: entry.CampaignID,
			LeadID:     l.ID,
			Number:     l.Number,
			Name:       l.Name,
			Variables:  entry.Variables,
			Status:     string(entry.Status),
			Metadata:   phoneInput.Metadata,
			CreatedAt:  entry.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  entry.UpdatedAt.Format(time.RFC3339),
		})
	}

	return output, nil
}
