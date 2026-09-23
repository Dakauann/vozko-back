package container

import (
	"log"

	"gorm.io/gorm"

	"vozko/domain/shared"
	stage_domain "vozko/domain/stage"
	instagram_repository "vozko/infra/repositories/instagram"
	stage_repository "vozko/infra/repositories/stage"
	telegram_repository "vozko/infra/repositories/telegram"
	unofficial_whatsapp_campaign_repository "vozko/infra/repositories/unofficial_whatsapp_campaign"
	whatsapp_campaign_repository "vozko/infra/repositories/whatsapp_campaign"
)

func containerPipelineResolvers(db *gorm.DB) map[shared.EntryType]stage_domain.ContainerPipelineResolver {
	return map[shared.EntryType]stage_domain.ContainerPipelineResolver{
		shared.EntryTypeWhatsApp:           whatsapp_campaign_repository.NewContainerPipelineResolver(db),
		shared.EntryTypeInstagram:          instagram_repository.NewContainerPipelineResolver(db),
		shared.EntryTypeTelegram:           telegram_repository.NewContainerPipelineResolver(db),
		shared.EntryTypeUnofficialWhatsApp: unofficial_whatsapp_campaign_repository.NewContainerPipelineResolver(db),
	}
}

func (c *Container) wireContainerPipelines() {
	registrar, ok := c.repositories.stage.(stage_repository.PipelineRegistrar)
	if !ok {
		log.Printf("[stage] the stage repository cannot register pipeline resolvers; channels fall back to the default funnel")
		return
	}

	for entryType, resolver := range containerPipelineResolvers(c.db) {
		registrar.SetContainerPipelineResolver(entryType, resolver)
	}
}
