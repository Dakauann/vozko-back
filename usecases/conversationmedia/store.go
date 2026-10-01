package conversationmedia

import (
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/media"
)

type store struct {
	files     media.FileStorage
	rows      conversation.ConversationMediaRepository
	inspector conversation.MediaInspector
}

func NewStore(files media.FileStorage, rows conversation.ConversationMediaRepository, inspector conversation.MediaInspector) conversation.MediaStore {
	return &store{files: files, rows: rows, inspector: inspector}
}

func (s *store) Store(input conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	key := strings.TrimSpace(input.Key)
	switch {
	case len(input.Data) == 0:
		return nil, conversation.ErrMediaRequired
	case key == "" || strings.TrimSpace(input.ID) == "":
		return nil, conversation.ErrMediaURLRequired
	}
	record := &conversation.ConversationMedia{
		ID:               input.ID,
		EntryID:          input.EntryID,
		EntryType:        input.EntryType,
		Type:             input.Type,
		MimeType:         input.MimeType,
		URL:              s.files.GetFileURL(key),
		OriginalFilename: input.OriginalFilename,
		SizeBytes:        int64(len(input.Data)),
		WhatsAppMediaID:  input.WhatsAppMediaID,
		CreatedAt:        time.Now().UTC(),
	}
	record.Normalize()
	if err := record.Validate(); err != nil {
		return nil, err
	}
	if err := s.files.UploadFile(key, input.Data, input.MimeType); err != nil {
		return nil, err
	}
	record.Layout = s.inspector.Inspect(input.Data, input.Type)
	if err := s.rows.Create(record); err != nil {
		return nil, err
	}
	return record, nil
}
