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

// CallPermissionStatus is a lightweight, read-only view of a lead's WhatsApp
// call-permission state for a conversation. It lets the UI enable or disable the
// "call via WhatsApp" action before a call is attempted. CanCall is true only
// when a permission is granted and still within its validity window.
type CallPermissionStatus struct {
	Status    string     `json:"status"` // "none" | "pending" | "granted" | "rejected" | "expired"
	CanCall   bool       `json:"can_call"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type RequestCallPermissionUseCase interface {
	RequestCallPermission(input RequestCallPermissionInput) (*Message, error)
	// CallPermissionStatus reports whether the conversation's lead currently
	// allows WhatsApp calls. A missing or lapsed permission yields CanCall=false.
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

// SearchMessagesByEntryUseCase runs a full-text/message search within one
// conversation entry, returning the page of matches and the total count.
type SearchMessagesByEntryUseCase interface {
	Execute(input SearchMessagesByEntryInput) ([]*Message, int64, error)
}
