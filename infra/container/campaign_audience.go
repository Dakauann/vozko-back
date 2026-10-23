package container

import (
	"log"

	campaignaudiencehttp "vozko/delivery/http/campaignaudiencehttp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/campaignaudiencewiring"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	"vozko/usecases/campaigncreate"
)

func (c *Container) campaignAudienceModule() *campaignaudiencewiring.Module {
	if c.campaignAudienceWiring != nil {
		return c.campaignAudienceWiring
	}
	if c.services.conversationAuthImpl == nil || c.useCases.workspaceTemplates == nil {
		log.Fatalf("[campaign-audience] the authorizer and the workspace templates must be wired before the audience step")
	}
	module, err := campaignaudiencewiring.New(c.db, campaignaudiencewiring.Adapters{
		Selections:  c.leadSelection(),
		Places:      c.leadSelection(),
		Leads:       c.repositories.lead,
		Definitions: c.repositories.customField,
		Templates:   c.useCases.workspaceTemplates,
		Gate:        c.sharedAnalyticsGate(),
		Shared:      c.redisProvider.SharedState(),
		EmailsSeen:  c.emailHygiene().Service,
	})
	if err != nil {
		log.Fatalf("[campaign-audience] %v", err)
	}
	c.campaignAudienceWiring = module
	official, err := campaignaudience_usecase.NewOfficialChannel(c.campaignAudienceDeps())
	if err != nil {
		log.Fatalf("[campaign-audience] official channel: %v", err)
	}
	module.Service.AddChannel(official)
	return module
}

func (c *Container) campaignAudienceDeps() campaignaudience_usecase.WhatsAppDeps {
	return campaignaudience_usecase.WhatsAppDeps{
		Screener:    c.campaignEligibility(),
		Subjects:    campaigncreate.SubjectSource{Leads: c.repositories.lead, Contacts: c.repositories.lead, Names: c.actorNames()},
		Definitions: c.repositories.customField,
		Templates:   c.useCases.workspaceTemplates,
		Suggestions: c.campaignAudienceWiring.Suggestions,
		Scopes:      c.services.conversationAuthImpl,
		Automation:  c.campaignAutomation(),
	}
}

func (c *Container) campaignAudience() *campaignaudience_usecase.Service {
	return c.campaignAudienceModule().Service
}

func (c *Container) addUnofficialAudience(instances uwc.CampaignInstanceUseCase) {
	c.campaignAudienceModule()
	deps := c.campaignAudienceDeps()
	deps.Instances = instances
	unofficial, err := campaignaudience_usecase.NewUnofficialChannel(deps)
	if err != nil {
		log.Fatalf("[campaign-audience] unofficial channel: %v", err)
	}
	c.campaignAudience().AddChannel(unofficial)
}

func (c *Container) campaignAudienceHandler() *campaignaudiencehttp.Handler {
	return campaignaudiencehttp.NewHandler(c.campaignAudience())
}
