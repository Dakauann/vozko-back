package container

import (
	"time"

	conversation_domain "vozko/domain/conversation"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
	conversation_repository "vozko/infra/repositories/conversation"
	"vozko/usecases/conversationad"
)

const (
	adImageDownloadTimeout = 20 * time.Second
	adImageMaxBytes        = 8 << 20
)

func (c *Container) adOrigins() conversation_domain.AdOriginRepository {
	if c.services.adOrigins == nil {
		c.services.adOrigins = conversation_repository.NewAdOriginRepository(c.db)
	}
	return c.services.adOrigins
}

func (c *Container) adOriginRecorder() *conversationad.Recorder {
	if c.services.adOriginRecorder == nil {
		c.services.adOriginRecorder = conversationad.NewRecorder(
			c.adOrigins(),
			c.mediaStore(),
			remotefile.NewFetcher(netguard.NewHTTPClient(adImageDownloadTimeout), adImageMaxBytes),
			c.services.conversationHub,
			time.Now,
		)
	}
	return c.services.adOriginRecorder
}

func (c *Container) adOriginReader() conversation_domain.AdOriginReader {
	return conversationad.NewReader(c.adOrigins(), c.repositories.conversationMedia)
}
