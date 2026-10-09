package container

import (
	"log"

	balance_domain "vozko/domain/balance"
	uwc_domain "vozko/domain/unofficial_whatsapp_campaign"
	wc_domain "vozko/domain/whatsapp_campaign"
	leadsend_repository "vozko/infra/repositories/leadsend"
	wc_repository "vozko/infra/repositories/whatsapp_campaign"
	agent_usecase "vozko/usecases/agent"
	"vozko/usecases/campaignautomation"
	leadsend_usecase "vozko/usecases/leadsend"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
	wc_usecase "vozko/usecases/whatsapp_campaign"
	workflow_usecase "vozko/usecases/workflow"
	workspace_department_usecase "vozko/usecases/workspace_department"
)

func (c *Container) campaignAutomation() *campaignautomation.Step {
	step, err := campaignautomation.NewStep(
		workflow_usecase.NewGetWorkflowUseCase(c.repositories.workflow),
		agent_usecase.NewGetAgentUseCase(c.repositories.agent),
	)
	if err != nil {
		log.Fatalf("[campaigns] the workflow and agent checks cannot be built: %v", err)
	}
	return step
}

func (c *Container) automateCampaignChanges(name string, useCase any) any {
	automated, ok := useCase.(interface {
		SetAutomation(campaignautomation.Checker)
	})
	if !ok {
		log.Fatalf("[campaigns] the %s cannot take the workflow and agent checks", name)
	}
	automated.SetAutomation(c.campaignAutomation())
	return useCase
}

func (c *Container) wireOfficialCampaignCreate(create wc_domain.CreateCampaignUseCase) {
	configurable, ok := create.(interface {
		SetAutomation(wc_usecase.AutomationCheck)
		SetIdempotentCampaigns(wc_usecase.IdempotentCampaigns)
	})
	if !ok {
		log.Fatalf("[campaigns] the official campaign creation cannot take the workflow checks and the idempotency keys")
	}
	keyed, ok := wc_repository.NewRepository(c.db).(wc_usecase.IdempotentCampaigns)
	if !ok {
		log.Fatalf("[campaigns] the official campaign repository cannot read idempotency keys")
	}
	configurable.SetAutomation(c.campaignAutomation())
	configurable.SetIdempotentCampaigns(keyed)
}

func (c *Container) wireUnofficialCampaignCreate(bundle *unofficialWhatsAppCampaignBundle) {
	configurable, ok := bundle.Create.(interface {
		SetAutomation(uwcuc.AutomationCheck)
		SetLeadsByID(uwcuc.LeadsByID)
		SetIdempotentCampaigns(uwcuc.IdempotentCampaigns)
	})
	if !ok {
		log.Fatalf("[unofficial-whatsapp-campaigns] the campaign creation cannot take the workflow checks, lead targets and idempotency keys")
	}
	keyed, ok := bundle.Campaigns.(uwcuc.IdempotentCampaigns)
	if !ok {
		log.Fatalf("[unofficial-whatsapp-campaigns] the campaign repository cannot read idempotency keys")
	}
	configurable.SetAutomation(c.campaignAutomation())
	configurable.SetLeadsByID(c.repositories.lead)
	configurable.SetIdempotentCampaigns(keyed)
}

func (c *Container) unofficialSends() *leadsend_usecase.Unofficial {
	bundle := c.unofficialWhatsAppCampaigns
	if bundle == nil || !bundle.Enabled {
		return nil
	}
	reviewed, ok := bundle.Actions.(uwc_domain.ReviewedStartUseCase)
	if !ok {
		log.Fatalf("[lead-sends] the unofficial campaign actions cannot start a reviewed send")
	}
	return &leadsend_usecase.Unofficial{
		Create: bundle.Create, Access: bundle.Access, Start: reviewed,
		Instances: bundle.Instances, Scopes: c.services.conversationAuthImpl,
	}
}

func (c *Container) buildLeadSends() *leadsend_usecase.Service {
	if c.services.conversationAuthImpl == nil || c.useCases.createWCCampaign == nil || c.useCases.wcCampaignAccess == nil || c.useCases.startWCCampaign == nil || c.services.metrics == nil {
		log.Fatalf("[lead-sends] the authorizer, the official campaign use cases and metrics must be wired before sends from a selection")
	}
	caps, ok := c.repositories.monthlySendCaps.(balance_domain.MonthlySendCapUsageReader)
	if !ok {
		log.Fatalf("[lead-sends] the monthly send cap repository cannot read what is left of the cap")
	}
	reviewedStart, ok := c.useCases.startWCCampaign.(wc_domain.ReviewedStartUseCase)
	if !ok {
		log.Fatalf("[lead-sends] the official campaign start cannot start a reviewed send")
	}
	campaignCache, ok := c.repositories.wcCampaign.(leadsend_usecase.CampaignCache)
	if !ok {
		log.Fatalf("[lead-sends] the official campaign repository cannot forget a cancelled send")
	}
	service, err := leadsend_usecase.NewService(leadsend_usecase.Deps{
		Permissions:         c.services.conversationAuthImpl,
		Snapshots:           c.leadSelection(),
		Leads:               c.repositories.lead,
		Contacts:            c.repositories.lead,
		Names:               c.actorNames(),
		Definitions:         c.repositories.customField,
		Store:               leadsend_repository.NewStore(c.db),
		Gate:                c.sharedAnalyticsGate(),
		Departments:         c.repositories.workspaceDepartment,
		CreationDepartments: workspace_department_usecase.NewResolveCreationDepartmentUseCase(c.repositories.workspace, c.repositories.workspaceDepartment),
		Templates:           c.useCases.workspaceTemplates,
		Costs:               c.useCases.consumeWhatsappTemplate,
		Balances:            c.services.cachedBalanceChecker,
		Caps:                caps,
		Official: leadsend_usecase.Official{
			Create: c.useCases.createWCCampaign, Access: c.useCases.wcCampaignAccess, Start: reviewedStart,
			Cache: campaignCache,
		},
		Unofficial: c.unofficialSends(),
		Metrics:    c.services.metrics,
	})
	if err != nil {
		log.Fatalf("[lead-sends] %v", err)
	}
	return service
}
