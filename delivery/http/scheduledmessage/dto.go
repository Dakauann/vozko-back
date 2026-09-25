package scheduledmessage

import (
	"time"

	sm "vozko/domain/scheduled_message"
)

type ScheduleMessageRequest struct {
	Text             string    `json:"text" example:"Bom dia! Seguindo nossa conversa..."`
	ScheduledAt      time.Time `json:"scheduled_at" example:"2026-08-13T14:30:00-03:00"`
	MediaID          *string   `json:"media_id,omitempty" example:"med_a1b2c3"`
	MediaType        *string   `json:"media_type,omitempty" example:"image"`
	ReplyToMessageID *string   `json:"reply_to_message_id,omitempty" example:"msg_a1b2c3"`
	Signed           *bool     `json:"signed,omitempty" example:"true"`

	Template *ScheduleTemplateRequest `json:"template,omitempty"`
}

type ScheduleTemplateRequest struct {
	TemplateID   string   `json:"template_id" example:"4f1c2a9e-8b7d-4e21-9a3f-2c6d5e8f1b0a"`
	BodyParams   []string `json:"body_params,omitempty" example:"Ana"`
	HeaderParams []string `json:"header_params,omitempty" example:"Pedido 42"`
}

type RescheduleMessageRequest struct {
	ScheduledAt time.Time `json:"scheduled_at" example:"2026-08-13T16:00:00-03:00"`
}

type WindowResponse struct {
	Open            bool       `json:"open"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	LatestAllowedAt *time.Time `json:"latestAllowedAt,omitempty"`

	TemplateLatestAllowedAt *time.Time `json:"templateLatestAllowedAt,omitempty"`
}

type ScheduledMessageResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspaceId"`
	EntryID         string `json:"entryId"`
	EntryType       string `json:"entryType"`
	CreatedByUserID string `json:"createdByUserId"`

	Kind     string                     `json:"kind" example:"template"`
	Template *ScheduledTemplateResponse `json:"template,omitempty"`

	Text             string  `json:"text,omitempty"`
	MediaID          *string `json:"mediaId,omitempty"`
	MediaType        *string `json:"mediaType,omitempty"`
	ReplyToMessageID *string `json:"replyToMessageId,omitempty"`
	Signed           bool    `json:"signed"`

	ScheduledAt time.Time `json:"scheduledAt"`

	Status        string  `json:"status"`
	FailureReason *string `json:"failureReason,omitempty"`
	FailureDetail string  `json:"failureDetail,omitempty"`

	SentAt        *time.Time `json:"sentAt,omitempty"`
	SentMessageID *string    `json:"sentMessageId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ScheduledTemplateResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name" example:"follow_up"`
	Preview      string   `json:"preview,omitempty" example:"Oi Ana, tudo certo com o seu pedido?"`
	BodyParams   []string `json:"bodyParams,omitempty"`
	HeaderParams []string `json:"headerParams,omitempty"`
}

type ScheduledMessageEnvelope struct {
	ScheduledMessage ScheduledMessageResponse `json:"scheduledMessage"`
	Window           WindowResponse           `json:"window"`
}

type ScheduledMessageListResponse struct {
	ScheduledMessages []ScheduledMessageResponse `json:"scheduledMessages"`
	Window            WindowResponse             `json:"window"`
}

type WorkspaceScheduledMessagesResponse struct {
	ScheduledMessages []ScheduledMessageResponse `json:"scheduledMessages"`
	Page              int                        `json:"page"`
	PageSize          int                        `json:"page_size"`
	TotalItems        int64                      `json:"total_items"`
	TotalPages        int                        `json:"total_pages"`
}

type WindowErrorResponse struct {
	Error   bool           `json:"error"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Window  WindowResponse `json:"window"`
}

func toWindowResponse(w sm.WindowState) WindowResponse {
	return WindowResponse{
		Open:                    w.Open,
		ExpiresAt:               w.ExpiresAt,
		LatestAllowedAt:         w.LatestAllowedAt,
		TemplateLatestAllowedAt: w.TemplateLatestAllowedAt,
	}
}

func toResponse(m *sm.ScheduledMessage) ScheduledMessageResponse {
	out := ScheduledMessageResponse{
		ID:               m.ID,
		WorkspaceID:      m.WorkspaceID,
		EntryID:          m.EntryID,
		EntryType:        string(m.EntryType),
		CreatedByUserID:  m.CreatedByUserID,
		Kind:             string(m.Kind),
		Template:         toTemplateResponse(m.Template),
		Text:             m.Text,
		MediaID:          m.MediaID,
		MediaType:        m.MediaType,
		ReplyToMessageID: m.ReplyToMessageID,
		Signed:           m.Signed,
		ScheduledAt:      m.ScheduledAt,
		Status:           string(m.Status),
		FailureDetail:    m.FailureDetail,
		SentAt:           m.SentAt,
		SentMessageID:    m.SentMessageID,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
	if m.FailureReason != nil {
		reason := string(*m.FailureReason)
		out.FailureReason = &reason
	}
	return out
}

func toTemplateResponse(t *sm.TemplateContent) *ScheduledTemplateResponse {
	if t == nil {
		return nil
	}
	return &ScheduledTemplateResponse{
		ID:           t.ID,
		Name:         t.Name,
		Preview:      t.Preview,
		BodyParams:   t.BodyParams,
		HeaderParams: t.HeaderParams,
	}
}

func toResponses(messages []*sm.ScheduledMessage) []ScheduledMessageResponse {
	out := make([]ScheduledMessageResponse, 0, len(messages))
	for _, m := range messages {
		out = append(out, toResponse(m))
	}
	return out
}
