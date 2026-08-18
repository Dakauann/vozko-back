package whatsapp_campaign_usecase

import (
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace_phone_access"
)

type updateCampaignUseCase struct {
	campaignRepo      wc.Repository
	entryRepo         wce.Repository
	templateRepo      template.Repository
	businessPhoneRepo businessphone.Repository
	phoneAccessRepo   workspace_phone_access.Repository
}

func NewUpdateCampaignUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	templateRepo template.Repository,
	businessPhoneRepo businessphone.Repository,
	phoneAccessRepo workspace_phone_access.Repository,
) wc.UpdateCampaignUseCase {
	return &updateCampaignUseCase{
		campaignRepo:      campaignRepo,
		entryRepo:         entryRepo,
		templateRepo:      templateRepo,
		businessPhoneRepo: businessPhoneRepo,
		phoneAccessRepo:   phoneAccessRepo,
	}
}

func (uc *updateCampaignUseCase) Execute(campaignID string, input *wc.Campaign) (*wc.Campaign, error) {
	if campaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}
	if input == nil {
		return nil, wc.ErrCampaignNameRequired
	}

	existing, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return nil, err
	}

	existing.Name = input.Name
	if input.Type.IsValid() {
		existing.Type = input.Type
	}
	if existing.IsOrganic() {
		existing.TemplateID = ""
	} else if input.TemplateID != "" {
		existing.TemplateID = input.TemplateID
	}
	existing.AgentID = input.AgentID
	existing.WorkflowID = input.WorkflowID
	existing.PipelineID = input.PipelineID
	existing.EnableAgentResponses = input.EnableAgentResponses
	existing.EnableWorkflow = input.EnableWorkflow
	existing.EnableAnalysis = input.EnableAnalysis
	existing.EnableAutoStaging = input.EnableAutoStaging
	existing.EnableAutoMemory = input.EnableAutoMemory
	existing.PreferAudio = input.PreferAudio
	existing.AiModel = input.AiModel
	// Archive/unarchive flows go through this usecase (the handler loads the
	// campaign, flips Archived, and calls Execute). Copying it from input is
	// what makes archiving persist, matching the voice and SMS usecases.
	// Leaving it out silently drops the flag: the row vanishes from the list
	// optimistically but reappears on reload.
	existing.Archived = input.Archived

	if input.BusinessPhoneID != "" {
		existing.BusinessPhoneID = input.BusinessPhoneID
	}

	existing.Normalize()
	if err := existing.ValidateMetadata(); err != nil {
		return nil, err
	}

	var businessPhone *businessphone.WhatsAppBusinessPhoneNumber
	if input.BusinessPhoneID != "" && uc.businessPhoneRepo != nil {
		var bpErr error
		businessPhone, bpErr = uc.businessPhoneRepo.FindByID(input.BusinessPhoneID)
		if bpErr != nil || businessPhone == nil {
			return nil, wc.ErrCampaignBusinessPhoneNotFound
		}

		allowed, accessErr := hasTemporaryCampaignBusinessPhoneAccess(
			existing.WorkspaceID,
			input.BusinessPhoneID,
			businessPhone,
			uc.phoneAccessRepo,
		)
		if accessErr != nil {
			return nil, accessErr
		}
		if !allowed {
			return nil, wc.ErrCampaignBusinessPhoneNoAccess
		}
	} else if existing.BusinessPhoneID != "" && uc.businessPhoneRepo != nil {
		businessPhone, _ = uc.businessPhoneRepo.FindByID(existing.BusinessPhoneID)
	}

	if !existing.IsOrganic() {
		if uc.templateRepo == nil {
			return nil, wc.ErrCampaignTemplateNotFound
		}

		tmpl, err := uc.templateRepo.FindByID(existing.TemplateID)
		if err != nil {
			return nil, wc.ErrCampaignTemplateNotFound
		}

		if !tmpl.IsReadyToSend() {
			usabilityMsg := tmpl.GetUsabilityMessage()
			if usabilityMsg != "" {
				return nil, wc.NewTemplateNotReadyError(tmpl.Name, usabilityMsg)
			}
			return nil, wc.ErrCampaignTemplateNotApproved
		}

		if businessPhone != nil && tmpl.WABAId != "" && businessPhone.WABAId != "" {
			if tmpl.WABAId != businessPhone.WABAId {
				return nil, wc.ErrCampaignTemplatePhoneMismatch
			}
		} else if businessPhone != nil && tmpl.WABAId == "" {
			return nil, wc.ErrCampaignTemplatePhoneMismatch
		}
	}

	if err := uc.campaignRepo.Update(campaignID, existing); err != nil {
		return nil, err
	}

	saved, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return nil, err
	}

	if uc.entryRepo != nil {
		counts, err := uc.entryRepo.CountByStatus(campaignID)
		if err == nil {
			saved.Metrics = wc.NewCampaignMetrics(counts)
		}
	}

	return saved, nil
}
