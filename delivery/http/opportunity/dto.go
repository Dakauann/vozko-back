package opportunity

import (
	"time"

	opportunitydomain "vozko/domain/opportunity"
)

type CreateOpportunityRequest struct {
	LeadID       string         `json:"leadId,omitempty" example:"lead_a1b2c3"`
	PipelineID   string         `json:"pipelineId" example:"pipe_a1b2c3"`
	StageID      string         `json:"stageId" example:"stg_a1b2c3"`
	OwnerID      string         `json:"ownerId,omitempty" example:"usr_a1b2c3"`
	CarteiraID   string         `json:"carteiraId,omitempty" example:"cart_a1b2c3"`
	Title        string         `json:"title,omitempty" example:"Plano anual"`
	ValueCents   int64          `json:"valueCents,omitempty" example:"150000"`
	Currency     string         `json:"currency,omitempty" example:"BRL"`
	Source       string         `json:"source,omitempty" example:"whatsapp"`
	CloseDate    *time.Time     `json:"closeDate,omitempty"`
	CustomFields map[string]any `json:"customFields,omitempty"`

	ConversationAssigneeID string `json:"conversationAssigneeId,omitempty" example:"usr_d4e5f6"`
	LinkEntryID            string `json:"linkEntryId,omitempty" example:"call_a1b2c3"`
	LinkEntryType          string `json:"linkEntryType,omitempty" example:"whatsapp"`
}

type UpdateOpportunityRequest struct {
	Title        *string        `json:"title,omitempty" example:"Plano anual"`
	ValueCents   *int64         `json:"valueCents,omitempty" example:"150000"`
	Currency     *string        `json:"currency,omitempty" example:"BRL"`
	OwnerID      *string        `json:"ownerId,omitempty" example:"usr_a1b2c3"`
	CarteiraID   *string        `json:"carteiraId,omitempty" example:"cart_a1b2c3"`
	Source       *string        `json:"source,omitempty" example:"whatsapp"`
	CloseDate    *time.Time     `json:"closeDate,omitempty"`
	CustomFields map[string]any `json:"customFields,omitempty"`
	StageID      *string        `json:"stageId,omitempty" example:"stg_a1b2c3"`
	Status       *string        `json:"status,omitempty" example:"won"`
	LostReasonID *string        `json:"lostReasonId,omitempty" example:"lr_a1b2c3"`
}

type MoveStageRequest struct {
	StageID      string `json:"stageId" example:"stg_a1b2c3"`
	Status       string `json:"status,omitempty" example:"won"`
	LostReasonID string `json:"lostReasonId,omitempty" example:"lr_a1b2c3"`
}

type LinkConversationRequest struct {
	EntryID   string `json:"entryId" example:"call_a1b2c3"`
	EntryType string `json:"entryType" example:"whatsapp"`
}

func toOpportunityStatus(value *string) *opportunitydomain.Status {
	if value == nil {
		return nil
	}
	converted := opportunitydomain.Status(*value)
	return &converted
}
