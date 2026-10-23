package container

import (
	conversation_domain "vozko/domain/conversation"
	"vozko/infra/mediaprobe"
	conversation_repository "vozko/infra/repositories/conversation"
	media_repository "vozko/infra/repositories/media"
	"vozko/usecases/conversationmedia"
)

func (c *Container) mediaStore() conversation_domain.MediaStore {
	if c.services.fileStorage == nil || c.repositories.conversationMedia == nil {
		return nil
	}
	if c.services.conversationMediaStore == nil {
		c.services.conversationMediaStore = conversationmedia.NewStore(c.services.fileStorage, c.repositories.conversationMedia, mediaprobe.New())
	}
	return c.services.conversationMediaStore
}

func (c *Container) mediaURLResolver() *conversation_domain.MediaURLResolver {
	if c.s3 == nil || c.db == nil {
		return nil
	}
	return &conversation_domain.MediaURLResolver{
		Storage: c.s3,
		Library: media_repository.NewMediaRepository(c.db),
		Uploads: conversation_repository.NewMediaURLReader(c.db),
	}
}
