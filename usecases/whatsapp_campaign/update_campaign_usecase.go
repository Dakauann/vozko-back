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
	automation        campaignautomation.Rules
	exitRisk          ExitRiskGate
	templateGrants    TemplateGrants
}

func (uc *updateCampaignUseCase) SetTemplateGrants(grants TemplateGrants) {
	uc.templateGrants = grants
}

func (uc *updateCampaignUseCase) SetExitRiskGate(gate ExitRiskGate) {
	uc.exitRisk = gate
}

func (uc *updateCampaignUseCase) SetAutomation(automation campaignautomation.Rules) {
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

	change := sendingChange{
		template: input.TemplateID != "" && input.TemplateID != existing.TemplateID,
		phone:    input.BusinessPhoneID != "" && input.BusinessPhoneID != existing.BusinessPhoneID,
	}
	automationBefore := existing.Automation()

	existing.Name = input.Name
	if input.Type.IsValid() {
		existing.Type = input.Type
	}
	if change.template {
		existing.TemplateID = input.TemplateID
	}
	if change.phone {
		existing.BusinessPhoneID = input.BusinessPhoneID
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
	if err := existing.Reschedule(input.ScheduledStart); err != nil {
		return nil, err
	}

	existing.Normalize()
	if err := existing.ValidateMetadata(); err != nil {
		return nil, err
	}
	if err := campaignautomation.RequireStored(uc.automation, existing.WorkspaceID, automationBefore, existing.Automation(), uc.storedAutomationEntries(campaignID)); err != nil {
		return nil, err
	}

	admission, err := uc.admitSendingChange(ctx, existing, change, input.ExitRiskAcknowledgedBy)
	if err != nil {
		return nil, err
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

func (uc *updateCampaignUseCase) SetArchived(_ context.Context, campaignID string, archived bool) error {
	if campaignID == "" {
		return wc.ErrCampaignNotFound
	}
	existing, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return err
	}
	if existing.IsOrganic() {
		return wc.ErrReceptiveManagedByNumber
	}
	existing.Archived = archived
	return uc.campaignRepo.Update(campaignID, existing)
}

type sendingChange struct {
	template bool
	phone    bool
}

func (c sendingChange) any() bool {
	return c.template || c.phone
}

func (uc *updateCampaignUseCase) admitSendingChange(ctx context.Context, edited *wc.Campaign, change sendingChange, acknowledgedBy string) (*buttonaction.CampaignAdmission, error) {
	if !change.any() {
		return nil, nil
	}
	businessPhone, err := uc.sendingPhone(edited, change.phone)
	if err != nil {
		return nil, err
	}
	if uc.templateRepo == nil {
		return nil, wc.ErrCampaignTemplateNotFound
	}
	if change.template && !uc.granted(edited.WorkspaceID, edited.TemplateID) {
		return nil, wc.ErrCampaignTemplateNotFound
	}
	tmpl, err := uc.templateRepo.FindByID(edited.TemplateID)
	if err != nil {
		return nil, wc.ErrCampaignTemplateNotFound
	}
	if !tmpl.IsReadyToSend() {
		if usabilityMsg := tmpl.GetUsabilityMessage(); usabilityMsg != "" {
			return nil, wc.NewTemplateNotReadyError(tmpl.Name, usabilityMsg)
		}
		return nil, wc.ErrCampaignTemplateNotApproved
	}
	if businessPhone != nil && (tmpl.WABAId == "" || (businessPhone.WABAId != "" && tmpl.WABAId != businessPhone.WABAId)) {
		return nil, wc.ErrCampaignTemplatePhoneMismatch
	}
	if !change.template {
		return nil, nil
	}
	if err := uc.requireFillableTemplate(edited.ID, tmpl.ParameterCount()); err != nil {
		return nil, err
	}
	return uc.admitTemplate(ctx, edited.WorkspaceID, tmpl, acknowledgedBy)
}

func (uc *updateCampaignUseCase) sendingPhone(edited *wc.Campaign, changed bool) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if uc.businessPhoneRepo == nil || edited.BusinessPhoneID == "" {
		return nil, nil
	}
	businessPhone, err := uc.businessPhoneRepo.FindByID(edited.BusinessPhoneID)
	if !changed {
		if err != nil {
			return nil, nil
		}
		return businessPhone, nil
	}
	if err != nil || businessPhone == nil {
		return nil, wc.ErrCampaignBusinessPhoneNotFound
	}
	allowed, err := hasTemporaryCampaignBusinessPhoneAccess(edited.WorkspaceID, edited.BusinessPhoneID, businessPhone, uc.phoneAccessRepo)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, wc.ErrCampaignBusinessPhoneNoAccess
	}
	return businessPhone, nil
}

func (uc *updateCampaignUseCase) storedAutomationEntries(campaignID string) campaignautomation.StoredEntries {
	if uc.entryRepo == nil {
		return nil
	}
	return func(visit func(campaign.EntryMetadata) error) error {
		return uc.entryRepo.EachStored(campaignID, func(entry wce.StoredEntry) error {
			return visit(entry.AutomationMetadata())
		})
	}
}

func (uc *updateCampaignUseCase) requireFillableTemplate(campaignID string, requiredParams int) error {
	if requiredParams == 0 {
		return nil
	}
	if uc.entryRepo == nil {
		return fmt.Errorf("campaign %s: the saved entries cannot be read to check the new template", campaignID)
	}
	return uc.entryRepo.EachStored(campaignID, func(entry wce.StoredEntry) error {
		if !entry.AwaitsSending() {
			return nil
		}
		return wc.RequireTemplateVariables(requiredParams, entry.Variables)
	})
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
