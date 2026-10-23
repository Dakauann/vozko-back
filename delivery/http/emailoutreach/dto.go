package emailoutreach

import "time"

type StartConversationRequest struct {
	LeadID         string `json:"lead_id,omitempty" example:"9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4"`
	Address        string `json:"address,omitempty" example:"ana@cliente.com.br"`
	SenderID       string `json:"sender_id,omitempty"`
	Subject        string `json:"subject" example:"Proposta para {{first_name}}"`
	Text           string `json:"text,omitempty" example:"Oi {{first_name}}, segue a proposta."`
	DesignID       string `json:"design_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key" example:"3f6c2a9e-8d1b-4c7a-9e2f-1a5b6c7d8e9f"`
}

type StartedConversationResponse struct {
	ConversationID string `json:"conversation_id"`
	EntryType      string `json:"entry_type" example:"email"`
	MessageID      string `json:"message_id"`
	Repeated       bool   `json:"repeated"`
	Recorded       bool   `json:"recorded"`
}

type QuoteResponse struct {
	UnitMicros            int64     `json:"unit_micros" example:"583"`
	UnitBRL               float64   `json:"unit_brl" example:"0.003498"`
	ExchangeRate          float64   `json:"exchange_rate" example:"6"`
	ExchangeRateUpdatedAt time.Time `json:"exchange_rate_updated_at" example:"2026-10-20T09:30:00Z"`
}
