package whatsapp_campaign_usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/domain/workspace_phone_access"
	"vozko/usecases/campaignautomation"
	"vozko/usecases/campaigncreate"
	"vozko/usecases/campaignguard"
)

type createCampaignUseCase struct {
	campaignRepo       wc.Repository
	entryRepo          wce.Repository
	leadRepo           lead.Repository
	templateRepo       template.Repository
	businessPhoneRepo  businessphone.Repository
	phoneAccessRepo    workspace_phone_access.Repository
	eligibility        campaignguard.Screener
	departmentResolver workspace_department.CreationDepartmentResolver
	templateGrants     TemplateGrants
	automation         AutomationCheck
	keyed              IdempotentCampaigns
}

type TemplateGrants interface {
	HasAccess(workspaceID, templateID string) (bool, error)
}

type AutomationCheck = campaignautomation.Checker

type IdempotentCampaigns interface {
	FindByIdempotencyKey(workspaceID, key string) (*wc.Campaign, error)
	CreateWithEntries(c *wc.Campaign, entries []wce.WhatsAppCampaignEntry) error
}

func (uc *createCampaignUseCase) SetTemplateGrants(grants TemplateGrants) {
	uc.templateGrants = grants
}

func (uc *createCampaignUseCase) SetAutomation(automation AutomationCheck) {
	uc.automation = automation
}

func (uc *createCampaignUseCase) SetIdempotentCampaigns(keyed IdempotentCampaigns) {
	uc.keyed = keyed
}

func NewCreateCampaignUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	leadRepo lead.Repository,
	templateRepo template.Repository,
	businessPhoneRepo businessphone.Repository,
	phoneAccessRepo workspace_phone_access.Repository,
	eligibility campaignguard.Screener,
	departmentResolver ...workspace_department.CreationDepartmentResolver,
) wc.CreateCampaignUseCase {
	var resolver workspace_department.CreationDepartmentResolver
	if len(departmentResolver) > 0 {
		resolver = departmentResolver[0]
	}
	return &createCampaignUseCase{
		campaignRepo:       campaignRepo,
		entryRepo:          entryRepo,
		leadRepo:           leadRepo,
		templateRepo:       templateRepo,
		businessPhoneRepo:  businessPhoneRepo,
		phoneAccessRepo:    phoneAccessRepo,
		eligibility:        eligibility,
		departmentResolver: resolver,
	}
}

func (uc *createCampaignUseCase) Execute(ctx context.Context, input *wc.Campaign) (*wc.Campaign, error) {
	if input == nil {
		return nil, wc.ErrCampaignNameRequired
	}
	if input.IsOrganic() {
		return nil, wc.ErrReceptiveManagedByNumber
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

	existing, err := uc.existing(input)
	if err != nil || existing != nil {
		return existing, err
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

	if uc.templateGrants == nil {
		return nil, wc.ErrCampaignTemplateNotFound
	}
	if granted, err := uc.templateGrants.HasAccess(input.WorkspaceID, input.TemplateID); err != nil || !granted {
		return nil, wc.ErrCampaignTemplateNotFound
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

	if uc.automation == nil {
		return nil, campaign.ErrAutomationUnavailable
	}
	if err := uc.automation.Check(input.WorkspaceID, input.Automation(), input.EntryMetadata()); err != nil {
		return nil, err
	}

	if uc.eligibility == nil {
		return nil, campaignguard.ErrUnavailable
	}

	if input.ID == "" {
		input.ID = uuid.New().String()
	}

	targets, err := campaigncreate.Resolve(uc.leadRepo, uc.leadRepo, input.WorkspaceID, phoneTargets(input.PhoneInputs))
	if err != nil {
		return nil, err
	}

	screening, err := uc.eligibility.Screen(ctx, input.WorkspaceID, targets.LeadIDs(), input.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	entries := make([]wce.WhatsAppCampaignEntry, 0, len(input.PhoneInputs))
	seenLeads := make(map[string]struct{}, len(input.PhoneInputs))
	for _, phoneInput := range input.PhoneInputs {
		l := targets.Of(phoneTarget(phoneInput))
		if l == nil {
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
		markIneligible(&entry, campaign.FirstSkip(screening.Skipped[l.ID], phoneInput.Skip), screening.Detail(phoneInput.Missing))
		entries = append(entries, entry)
	}

	if err := uc.save(input, entries); err != nil {
		if errors.Is(err, campaign.ErrIdempotencyKeyTaken) {
			return uc.winnerOf(input)
		}
		return nil, err
	}

	return uc.hydrated(input.ID)
}

func (uc *createCampaignUseCase) save(input *wc.Campaign, entries []wce.WhatsAppCampaignEntry) error {
	if input.IdempotencyKey != "" {
		if uc.keyed == nil {
			return campaign.ErrIdempotencyUnavailable
		}
		return uc.keyed.CreateWithEntries(input, entries)
	}
	if err := uc.campaignRepo.Create(input); err != nil {
		return err
	}
	_, err := uc.entryRepo.CreateMany(entries)
	return err
}

func phoneTarget(in wc.PhoneInput) campaigncreate.Target {
	return campaigncreate.Target{LeadID: in.LeadID, Number: in.Number, Name: in.Name}
}

func phoneTargets(inputs []wc.PhoneInput) []campaigncreate.Target {
	targets := make([]campaigncreate.Target, 0, len(inputs))
	for _, in := range inputs {
		targets = append(targets, phoneTarget(in))
	}
	return targets
}

func (uc *createCampaignUseCase) existing(input *wc.Campaign) (*wc.Campaign, error) {
	found, ok, err := campaigncreate.FindKeyed[*wc.Campaign](uc.keyed, input.WorkspaceID, input.IdempotencyKey, wc.ErrCampaignNotFound)
	if err != nil || !ok {
		return nil, err
	}
	return uc.hydrated(found.ID)
}

func (uc *createCampaignUseCase) winnerOf(input *wc.Campaign) (*wc.Campaign, error) {
	winner, err := uc.existing(input)
	if err != nil {
		return nil, err
	}
	if winner == nil {
		return nil, campaign.ErrIdempotencyKeyTaken
	}
	return winner, nil
}

func (uc *createCampaignUseCase) hydrated(campaignID string) (*wc.Campaign, error) {
	saved, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	counts, err := uc.entryRepo.CountByStatus(campaignID)
	if err == nil {
		saved.Metrics = wc.NewCampaignMetrics(counts)
	}
	return saved, nil
}

func markIneligible(entry *wce.WhatsAppCampaignEntry, reason campaign.SkipReason, detail campaign.SkipDetail) {
	if reason == "" {
		return
	}
	entry.Status, entry.ErrorCode, entry.ErrorMessage = reason.OutcomeWith(detail)
}
