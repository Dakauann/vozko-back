package whatsapp_campaign_usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"vozko/domain/lead"
	lead_campaign_send "vozko/domain/lead_campaign_send"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workspace_department "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
	"vozko/domain/workspace_phone_access"
)

type createCampaignUseCase struct {
	campaignRepo         wc.Repository
	entryRepo            wce.Repository
	leadRepo             lead.Repository
	templateRepo         template.Repository
	businessPhoneRepo    businessphone.Repository
	phoneAccessRepo      workspace_phone_access.Repository
	workspaceConfigRepo  wsc.Repository
	leadCampaignSendRepo lead_campaign_send.Repository
	departmentResolver   workspace_department.CreationDepartmentResolver
}

func NewCreateCampaignUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	leadRepo lead.Repository,
	templateRepo template.Repository,
	businessPhoneRepo businessphone.Repository,
	phoneAccessRepo workspace_phone_access.Repository,
	workspaceConfigRepo wsc.Repository,
	leadCampaignSendRepo lead_campaign_send.Repository,
	departmentResolver ...workspace_department.CreationDepartmentResolver,
) wc.CreateCampaignUseCase {
	var resolver workspace_department.CreationDepartmentResolver
	if len(departmentResolver) > 0 {
		resolver = departmentResolver[0]
	}
	return &createCampaignUseCase{
		campaignRepo:         campaignRepo,
		entryRepo:            entryRepo,
		leadRepo:             leadRepo,
		templateRepo:         templateRepo,
		businessPhoneRepo:    businessPhoneRepo,
		phoneAccessRepo:      phoneAccessRepo,
		workspaceConfigRepo:  workspaceConfigRepo,
		leadCampaignSendRepo: leadCampaignSendRepo,
		departmentResolver:   resolver,
	}
}

func (uc *createCampaignUseCase) Execute(ctx context.Context, input *wc.Campaign) (*wc.Campaign, error) {
	if input == nil {
		return nil, wc.ErrCampaignNameRequired
	}

	input.Normalize()
	if err := input.Validate(); err != nil {
		return nil, err
	}

	if uc.departmentResolver != nil {
		departmentID, err := uc.departmentResolver.Resolve(ctx, input.WorkspaceID)
		if err != nil {
			return nil, err
		}
		input.DepartmentID = departmentID
	}

	var businessPhone *businessphone.WhatsAppBusinessPhoneNumber
	if input.BusinessPhoneID != "" {
		var bpErr error
		businessPhone, bpErr = uc.businessPhoneRepo.FindByID(input.BusinessPhoneID)
		if bpErr != nil || businessPhone == nil {
			return nil, wc.ErrCampaignBusinessPhoneNotFound
		}

		allowed, accessErr := hasTemporaryCampaignBusinessPhoneAccess(
			input.WorkspaceID,
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
	}

	if input.IsOrganic() {
		if input.ID == "" {
			input.ID = uuid.New().String()
		}
		if err := uc.campaignRepo.Create(input); err != nil {
			return nil, err
		}
		saved, err := uc.campaignRepo.FindByID(input.ID)
		if err != nil {
			return nil, err
		}
		saved.Metrics = wc.NewCampaignMetrics(nil)
		return saved, nil
	}

	if uc.templateRepo == nil {
		return nil, wc.ErrCampaignTemplateNotFound
	}

	tmpl, err := uc.templateRepo.FindByID(input.TemplateID)
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

	requiredParams := tmpl.ParameterCount()
	if err := input.ValidateTemplateVariables(requiredParams); err != nil {
		return nil, err
	}

	if input.ID == "" {
		input.ID = uuid.New().String()
	}

	if err := uc.campaignRepo.Create(input); err != nil {
		return nil, err
	}

	bulkInputs := make([]lead.BulkLeadInput, len(input.PhoneInputs))
	for i, phoneInput := range input.PhoneInputs {
		bulkInputs[i] = lead.BulkLeadInput{
			Number: phoneInput.Number,
			Name:   phoneInput.Name,
		}
	}

	leadsMap, err := uc.leadRepo.FindOrCreateMany(input.WorkspaceID, bulkInputs)
	if err != nil {
		return nil, err
	}

	entries := make([]wce.WhatsAppCampaignEntry, 0, len(input.PhoneInputs))
	seenLeads := make(map[string]struct{}, len(input.PhoneInputs))
	for _, phoneInput := range input.PhoneInputs {
		normalized := lead.NormalizeNumber(phoneInput.Number)
		l, found := leadsMap[normalized]
		if !found || l == nil {
			continue
		}
		if _, dup := seenLeads[l.ID]; dup {
			continue
		}
		seenLeads[l.ID] = struct{}{}

		entry := wce.WhatsAppCampaignEntry{
			ID:         uuid.New().String(),
			CampaignID: input.ID,
			LeadID:     l.ID,
			Status:     wce.SendStatusPending,
			Variables:  phoneInput.Variables,
			Metadata:   phoneInput.Metadata,
		}
		entries = append(entries, entry)
	}

	if _, err := uc.entryRepo.CreateMany(entries); err != nil {
		return nil, err
	}

	uc.markSpamEntries(entries, input.BusinessPhoneID, input.WorkspaceID)

	saved, err := uc.campaignRepo.FindByID(input.ID)
	if err != nil {
		return nil, err
	}

	counts, err := uc.entryRepo.CountByStatus(input.ID)
	if err == nil {
		saved.Metrics = wc.NewCampaignMetrics(counts)
	}

	return saved, nil
}

func (uc *createCampaignUseCase) markSpamEntries(entries []wce.WhatsAppCampaignEntry, businessPhoneID, workspaceID string) {
	if uc.workspaceConfigRepo == nil || uc.leadCampaignSendRepo == nil || businessPhoneID == "" {
		return
	}

	cfg, err := uc.workspaceConfigRepo.GetByWorkspaceID(context.Background(), workspaceID)
	if err != nil || cfg.CampaignSpamProtectionDays <= 0 {
		return
	}

	leadIDs := make([]string, len(entries))
	for i, e := range entries {
		leadIDs[i] = e.LeadID
	}

	lastSends, err := uc.leadCampaignSendRepo.GetLastSendTimesBatch(leadIDs, businessPhoneID)
	if err != nil {
		fmt.Printf("whatsapp campaign create: failed to check spam protection: %v\n", err)
		return
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -cfg.CampaignSpamProtectionDays)
	for _, entry := range entries {
		lastSent, exists := lastSends[entry.LeadID]
		if exists && lastSent.After(cutoff) {
			_ = uc.entryRepo.UpdateStatus(entry.ID, wce.SendStatusNotEligiblePossibleSpam, "", 0, "")
		}
	}
}
