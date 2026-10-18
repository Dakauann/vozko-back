package whatsapp_campaign_usecase

import (
	"context"
	"fmt"

	"vozko/domain/buttonaction"
	"vozko/domain/campaign"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace_phone_access"
	"vozko/usecases/campaignautomation"
)

type updateCampaignUseCase struct {
	campaignRepo      wc.Repository
	entryRepo         wce.Repository
	templateRepo      template.Repository
	businessPhoneRepo businessphone.Repository
	phoneAccessRepo   workspace_phone_access.Repository
	automation        AutomationCheck
	exitRisk          ExitRiskGate
	templateGrants    TemplateGrants
}

func (uc *updateCampaignUseCase) SetTemplateGrants(grants TemplateGrants) {
	uc.templateGrants = grants
}

func (uc *updateCampaignUseCase) SetExitRiskGate(gate ExitRiskGate) {
	uc.exitRisk = gate
}

func (uc *updateCampaignUseCase) SetAutomation(automation AutomationCheck) {
	uc.automation = automation
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

func (uc *updateCampaignUseCase) Execute(ctx context.Context, campaignID string, input *wc.Campaign) (*wc.Campaign, error) {
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
	if existing.IsOrganic() || input.IsOrganic() {
		return nil, wc.ErrReceptiveManagedByNumber
	}
	if err := campaign.RefuseSelectionChange(existing.Source, existing.ChangesWhatIsSent(input)); err != nil {
		return nil, err
	}

	templateChanged := input.TemplateID != "" && input.TemplateID != existing.TemplateID
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
	existing.AiModel = input.AiModel
	existing.Archived = input.Archived

	if input.BusinessPhoneID != "" {
		existing.BusinessPhoneID = input.BusinessPhoneID
	}

	existing.Normalize()
	if err := existing.ValidateMetadata(); err != nil {
		return nil, err
	}
	if err := campaignautomation.Require(uc.automation, existing.WorkspaceID, existing.Automation(), input.EntryMetadata()); err != nil {
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

	var admission *buttonaction.CampaignAdmission
	if !existing.IsOrganic() {
		if uc.templateRepo == nil {
			return nil, wc.ErrCampaignTemplateNotFound
		}
		if templateChanged && !uc.granted(existing.WorkspaceID, existing.TemplateID) {
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

		if templateChanged {
			if admission, err = uc.admitTemplate(ctx, existing.WorkspaceID, tmpl, input.ExitRiskAcknowledgedBy); err != nil {
				return nil, err
			}
		}
	}

	if err := uc.campaignRepo.Update(campaignID, existing); err != nil {
		return nil, err
	}
	if admission != nil {
		if err := uc.exitRisk.RecordCampaignAdmission(ctx, existing.WorkspaceID, campaignID, *admission); err != nil {
			return nil, fmt.Errorf("campaign %s was saved, but its new template's exit setup was not recorded: %w", campaignID, err)
		}
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

func (uc *updateCampaignUseCase) admitTemplate(ctx context.Context, workspaceID string, tmpl *template.Template, acknowledgedBy string) (*buttonaction.CampaignAdmission, error) {
	if uc.exitRisk == nil {
		return nil, buttonaction.ErrUnavailable
	}
	admission, err := uc.exitRisk.AdmitCampaign(ctx, workspaceID, tmpl, acknowledgedBy)
	if err != nil {
		return nil, err
	}
	return &admission, nil
}

func (uc *updateCampaignUseCase) granted(workspaceID, templateID string) bool {
	if uc.templateGrants == nil {
		return false
	}
	granted, err := uc.templateGrants.HasAccess(workspaceID, templateID)
	return err == nil && granted
}
