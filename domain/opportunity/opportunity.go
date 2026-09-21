package opportunity

import (
	"errors"
	"strings"
	"time"
)

type Status string

const (
	StatusOpen Status = "open"
	StatusWon  Status = "won"
	StatusLost Status = "lost"
)

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusWon || s == StatusLost
}

const DefaultCurrency = "BRL"

type Opportunity struct {
	ID           string         `json:"id"`
	WorkspaceID  string         `json:"workspaceId"`
	LeadID       string         `json:"leadId"`
	PipelineID   string         `json:"pipelineId"`
	StageID      string         `json:"stageId"`
	OwnerID      string         `json:"ownerId,omitempty"`
	CarteiraID   string         `json:"carteiraId,omitempty"`
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

func (o *Opportunity) IsClosed() bool {
	return o.Status == StatusWon || o.Status == StatusLost
}
