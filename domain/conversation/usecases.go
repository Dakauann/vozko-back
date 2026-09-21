package conversation

import (
	"context"
	"time"
)

type CreateMessageUseCase interface {
	Execute(message *Message) (*Message, error)
}

type UpdateMessageUseCase interface {
	Execute(messageID string, message *Message) (*Message, error)
}

type DeleteMessageUseCase interface {
	Execute(messageID string) error
}

type GetMessageUseCase interface {
	Execute(messageID string) (*Message, error)
}

type HandleWhatsAppMessageUseCase interface {
	Execute(ctx context.Context, payload *WhatsAppWebhookPayload) error
}

type ConsumeWhatsAppMessageWebhookUseCase interface {
	Start() error
}

type SendMessageInput struct {
	EntryID          string
	EntryType        string
	Text             string
	MediaID          *string
	MediaType        *MediaType
	SenderID         string
	ReplyToMessageID string
}

type SendConversationMessageUseCase interface {
	Execute(input SendMessageInput) (*Message, error)
}

type RequestCallPermissionInput struct {
	EntryID   string
	EntryType string
	SenderID  string
	BodyText  string
}

type CallPermissionStatus struct {
	Status    string     `json:"status"`
	CanCall   bool       `json:"can_call"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type RequestCallPermissionUseCase interface {
	RequestCallPermission(input RequestCallPermissionInput) (*Message, error)
	CallPermissionStatus(entryID, entryType string) (CallPermissionStatus, error)
}

type UploadMediaInput struct {
	EntryID   string
	EntryType string
	MediaType MediaType
	Filename  string
	Data      []byte
	MimeType  string
}

type UploadConversationMediaUseCase interface {
	Execute(input UploadMediaInput) (*ConversationMedia, error)
}

type GetConversationMediaUseCase interface {
	Execute(mediaID string) (*ConversationMedia, error)
}

type SearchMessagesByEntryUseCase interface {
	Execute(input SearchMessagesByEntryInput) ([]*Message, int64, error)
}
