package conversation

import "time"

type SendMessageRequest struct {
	Text             string  `json:"text" example:"Olá, como posso ajudar?"`
	MediaID          *string `json:"media_id,omitempty" example:"med_a1b2c3"`
	MediaType        *string `json:"media_type,omitempty" example:"image"`
	ReplyToMessageID string  `json:"reply_to_message_id,omitempty" example:"msg_a1b2c3"`
}

type CallPermissionRequestBody struct {
	BodyText string `json:"body_text,omitempty" example:"Podemos te ligar pelo WhatsApp?"`
}

type ReopenWindowRequest struct {
	TemplateID string   `json:"template_id" example:"tmpl_a1b2c3"`
	Parameters []string `json:"parameters,omitempty" example:"João,10/07"`
}

type MessageResponse struct {
	ID                string                 `json:"id"`
	EntryID           string                 `json:"entryId"`
	EntryType         string                 `json:"entryType"`
	Channel           string                 `json:"channel"`
	MessageType       string                 `json:"messageType"`
	From              string                 `json:"from"`
	To                string                 `json:"to"`
	Text              string                 `json:"text"`
	Image             []byte                 `json:"image,omitempty"`
	Video             []byte                 `json:"video,omitempty"`
	MediaID           *string                `json:"mediaId,omitempty"`
	MediaType         string                 `json:"mediaType,omitempty"`
	Read              bool                   `json:"read"`
	ReadAt            *time.Time             `json:"readAt,omitempty"`
	ReadBy            *string                `json:"readBy,omitempty"`
	WhatsAppMessageID *string                `json:"whatsappMessageId,omitempty"`
	ReplyToMessageID  *string                `json:"replyToMessageId,omitempty"`
	DeliveryStatus    string                 `json:"deliveryStatus,omitempty"`
	SenderName        string                 `json:"senderName,omitempty"`
	SenderAvatar      string                 `json:"senderAvatar,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
}

type MessageEnvelopeResponse struct {
	Message MessageResponse `json:"message"`
}

type SearchMessagesResponse struct {
	Messages   []MessageResponse `json:"messages"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalItems int64             `json:"total_items"`
	TotalPages int               `json:"total_pages"`
}
