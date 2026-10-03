package container

import (
	conversation_domain "vozko/domain/conversation"
	"vozko/infra/mediaprobe"
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
