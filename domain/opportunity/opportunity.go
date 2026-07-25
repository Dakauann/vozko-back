// Package opportunity defines the sales deal ("oportunidade"): the first-class
// record behind the Funil de Vendas. An Opportunity is workspace-global, belongs
// to a Pipeline + Stage, has an owner, a value, a close date and typed custom
// fields, and links to one or more conversations so the deal timeline shows the
// WhatsApp/voice history. It reuses the shared stage, label and inbox
// infrastructure rather than forking it.
package opportunity

import (
	"errors"
	"strings"
	"time"
)

// Status is the terminal-or-open state of a deal.
type Status string

const (
	StatusOpen Status = "open"
	StatusWon  Status = "won"
	StatusLost Status = "lost"
)

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusWon || s == StatusLost
}

// DefaultCurrency is the customer's local sales currency. Deal value is the
// customer's own sales figure; it is intentionally BRL by default for the
// Brazilian market.
const DefaultCurrency = "BRL"

// Opportunity is a sales deal.
//
// MONEY GUARDRAIL: ValueCents is the CUSTOMER's own sales amount (their R$ deal
// size). It is NOT Vozko platform money and must never be routed through the
// USD-micros wallet / saldo / ledger. It is a plain integer amount in the minor
// unit of Currency and never touches Asaas or FX.
type Opportunity struct {
	ID           string         `json:"id"`
	WorkspaceID  string         `json:"workspaceId"`
	LeadID       string         `json:"leadId"`
	PipelineID   string         `json:"pipelineId"`
	StageID      string         `json:"stageId"`
	OwnerID      string         `json:"ownerId,omitempty"`    // deal owner; seeded from the conversation assignee, then independent
	CarteiraID   string         `json:"carteiraId,omitempty"` // optional owner group / portfolio
	Title        string         `json:"title"`
	ValueCents   int64          `json:"valueCents"`
	Currency     string         `json:"currency"`
	Status       Status         `json:"status"`
	LostReasonID string         `json:"lostReasonId,omitempty"`
	Source       string         `json:"source,omitempty"`
	CloseDate    *time.Time     `json:"closeDate,omitempty"`
	CustomFields map[string]any `json:"customFields,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

var (
	ErrWorkspaceRequired = errors.New("opportunity: workspace is required")
	ErrPipelineRequired  = errors.New("opportunity: pipeline is required")
	ErrStageRequired     = errors.New("opportunity: stage is required")
	ErrTitleOrLead       = errors.New("opportunity: a title or a lead is required")
	ErrInvalidStatus     = errors.New("opportunity: invalid status")
	ErrNegativeValue     = errors.New("opportunity: value cannot be negative")
	ErrLostReasonMissing = errors.New("opportunity: a lost opportunity requires a lost reason")
)

// Normalize applies defaults a client may omit.
func (o *Opportunity) Normalize() {
	o.Title = strings.TrimSpace(o.Title)
	o.Currency = strings.ToUpper(strings.TrimSpace(o.Currency))
	if o.Currency == "" {
		o.Currency = DefaultCurrency
	}
	if o.Status == "" {
		o.Status = StatusOpen
	}
}

// Validate checks the deal is coherent. It enforces the "lost needs a reason"
// rule so losses stay reportable.
func (o *Opportunity) Validate() error {
	if strings.TrimSpace(o.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(o.PipelineID) == "" {
		return ErrPipelineRequired
	}
	if strings.TrimSpace(o.StageID) == "" {
		return ErrStageRequired
	}
	if strings.TrimSpace(o.Title) == "" && strings.TrimSpace(o.LeadID) == "" {
		return ErrTitleOrLead
	}
	if !o.Status.Valid() {
		return ErrInvalidStatus
	}
	if o.ValueCents < 0 {
		return ErrNegativeValue
	}
	if o.Status == StatusLost && strings.TrimSpace(o.LostReasonID) == "" {
		return ErrLostReasonMissing
	}
	return nil
}

// IsClosed reports whether the deal has reached a terminal outcome.
func (o *Opportunity) IsClosed() bool {
	return o.Status == StatusWon || o.Status == StatusLost
}
