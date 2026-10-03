package conversationad

import (
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type Reader struct {
	origins conversation.AdOriginRepository
	media   conversation.ConversationMediaRepository
}

func NewReader(origins conversation.AdOriginRepository, media conversation.ConversationMediaRepository) *Reader {
	return &Reader{origins: origins, media: media}
}

func (r *Reader) AdOrigin(entryID string, entryType shared.EntryType) (*conversation.AdOrigin, error) {
	origin, err := r.origins.Get(entryID, entryType)
	if err != nil || origin == nil || origin.ImageMediaID == "" {
		return origin, err
	}
	image, err := r.media.GetByID(origin.ImageMediaID)
	switch {
	case errors.Is(err, conversation.ErrMediaNotFound):
		return origin, nil
	case err != nil:
		return nil, err
	}
	if image.BelongsTo(entryID, entryType) {
		origin.Image = image.Attachment()
	}
	return origin, nil
}
