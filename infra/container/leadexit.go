package container

import (
	"log"

	"vozko/domain/leadexit"
	leadexit_repository "vozko/infra/repositories/leadexit"
	uwc_repo "vozko/infra/repositories/unofficial_whatsapp_campaign"
	ce_usecase "vozko/usecases/conversation_event"
	leadexit_usecase "vozko/usecases/leadexit"
)

func (c *Container) leadExitStep() leadexit.Step {
	if c.leadExitInboundStep != nil {
		return c.leadExitInboundStep
	}
	if c.services.assignmentService == nil || c.useCases.opportunity == nil || c.services.crmTelemetryPublisher == nil {
		log.Fatalf("[lead-exit] the inbound exit step needs the assignment service, the deal service and the timeline publisher")
	}
	sent, err := leadexit_usecase.NewSentTemplates(leadexit_usecase.SentTemplatesDeps{
		Entries:   c.repositories.wcEntry,
		Campaigns: c.repositories.wcCampaign,
		Messages:  c.repositories.conversation,
		Leads:     c.repositories.lead,
		Templates: c.repositories.whatsappTemplate,
	})
	if err != nil {
		log.Fatalf("[lead-exit] sent templates: %v", err)
	}
	campaigns, err := leadexit_usecase.NewUnofficialCampaignMessages(leadexit_usecase.UnofficialCampaignMessagesDeps{
		Entries:  uwc_repo.NewEntryRepository(c.db),
		Messages: c.repositories.conversation,
	})
	if err != nil {
		log.Fatalf("[lead-exit] unofficial campaign messages: %v", err)
	}
	step, err := leadexit_usecase.NewStep(leadexit_usecase.Deps{
		Buttons:     c.buttonActions(),
		Sent:        sent,
		Campaigns:   campaigns,
		Runs:        leadexit_repository.NewRunStore(c.db),
		Consent:     c.leadConsent(),
		Leads:       c.repositories.lead,
		HandOff:     c.services.assignmentService,
		Profiles:    c.leadProfiles(),
		Deals:       c.useCases.opportunity,
		Workflows:   c.tapWorkflows(),
		Delegations: c.buttonDelegations(),
		CallLists:   c.callLists(),
		Events:      ce_usecase.NewLogger(c.services.crmTelemetryPublisher),
	})
	if err != nil {
		log.Fatalf("[lead-exit] step: %v", err)
	}
	c.leadExitInboundStep = step
	return step
}

func (c *Container) wireLeadExit() {
	inbound, ok := c.useCases.handleWhatsAppMessage.(interface{ SetLeadExitStep(leadexit.Step) })
	if !ok {
		log.Fatalf("[lead-exit] the official WhatsApp inbound handler cannot take the exit step")
	}
	inbound.SetLeadExitStep(c.leadExitStep())
}
