package container

import (
	"vozko/domain/webhook"
	webhook_repository "vozko/infra/repositories/webhook"
)

func (c *Container) processedWebhookEvents() webhook.ProcessedEventRepository {
	return webhook_repository.NewProcessedEventRepository(c.db)
}
