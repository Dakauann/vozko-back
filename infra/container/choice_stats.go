package container

import (
	"log"

	choicestatshttp "vozko/delivery/http/choicestatshttp"
	"vozko/domain/choicestats"
	choicestats_repository "vozko/infra/repositories/choicestats"
	choicestats_usecase "vozko/usecases/choicestats"
)

func (c *Container) choiceStatsStore() *choicestats_repository.Store {
	if c.choiceStore == nil {
		c.choiceStore = choicestats_repository.New(c.db)
	}
	return c.choiceStore
}

func (c *Container) choiceStatsRecorder() *choicestats_usecase.Recorder {
	if c.choiceRecorder != nil {
		return c.choiceRecorder
	}
	recorder, err := choicestats_usecase.NewRecorder(c.choiceStatsStore(), c.attendanceScheduleResolver(), c.services.metrics, c.choiceClickTokens())
	if err != nil {
		log.Fatalf("[choice-stats] recorder: %v", err)
	}
	c.choiceRecorder = recorder
	return recorder
}

func (c *Container) choiceClickTokens() *choicestats.ClickTokens {
	if c.choiceTokens != nil {
		return c.choiceTokens
	}
	tokens, err := choicestats.NewClickTokens(c.cfg.AuthJWTSecret)
	if err != nil {
		log.Fatalf("[choice-stats] click tokens: %v", err)
	}
	c.choiceTokens = tokens
	return tokens
}

func (c *Container) choiceStatsHandler() *choicestatshttp.Handler {
	return choicestatshttp.NewHandler(c.choiceStatsReader(), c.services.conversationAuthImpl)
}

func (c *Container) choiceStatsReader() *choicestats_usecase.Reader {
	deps := choicestats_usecase.ReaderDeps{
		Store:        c.choiceStatsStore(),
		Zones:        c.attendanceScheduleResolver(),
		Workflows:    c.useCases.scopedWorkflows,
		Campaigns:    c.repositories.wcCampaign,
		Templates:    c.useCases.workspaceTemplates,
		TemplateRepo: c.repositories.whatsappTemplate,
		LinkBase:     c.cfg.ShortLinkBaseURL,
	}
	if c.unofficialWhatsAppCampaigns != nil && c.unofficialWhatsAppCampaigns.Enabled && c.unofficialWhatsAppCampaigns.Access != nil {
		deps.Unofficial = c.unofficialWhatsAppCampaigns.Access
	}
	reader, err := choicestats_usecase.NewReader(deps)
	if err != nil {
		log.Fatalf("[choice-stats] reader: %v", err)
	}
	return reader
}

func (c *Container) choiceButtonTracking() *choicestats_usecase.ButtonTracking {
	links, err := choicestats_usecase.NewTrackedLinks(c.useCases.createShortLink, c.cfg.ShortLinkBaseURL)
	if err != nil {
		log.Fatalf("[choice-stats] tracked links: %v", err)
	}
	tracking, err := choicestats_usecase.NewButtonTracking(links, c.repositories.shortLink)
	if err != nil {
		log.Fatalf("[choice-stats] button tracking: %v", err)
	}
	return tracking
}
