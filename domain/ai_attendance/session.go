// Package ai_attendance models AI agents as first-class attendants for CRM metrics.
package ai_attendance

import (
	"errors"
	"strings"
	"time"
)

type Outcome string

const (
	OutcomeContained  Outcome = "contained"
	OutcomeHandedOff  Outcome = "handed_off"
	OutcomeAbandoned  Outcome = "abandoned"
	OutcomeError      Outcome = "error"
	OutcomeSuppressed Outcome = "suppressed"
)

func (o Outcome) Valid() bool {
	switch o {
	case OutcomeContained, OutcomeHandedOff, OutcomeAbandoned, OutcomeError, OutcomeSuppressed:
		return true
	}
	return false
}

var (
	ErrAlreadyEnded      = errors.New("ai attendance: session already ended")
	ErrSessionNotFound   = errors.New("ai attendance: session not found")
	ErrWorkspaceRequired = errors.New("ai attendance: workspace required")
	ErrEntryRequired     = errors.New("ai attendance: entry required")
	ErrAgentRequired     = errors.New("ai attendance: agent required")
)

// Session is one AI-attendance span on a conversation (chat or voice).
type Session struct {
	ID                  string     `json:"id"`
	WorkspaceID         string     `json:"workspaceId"`
	EntryID             string     `json:"entryId"`
	EntryType           string     `json:"entryType"`
	AgentID             string     `json:"agentId"`
	Channel             string     `json:"channel"`
	CallID              string     `json:"callId,omitempty"`
	CampaignID          string     `json:"campaignId,omitempty"`
	StartedAt           time.Time  `json:"startedAt"`
	EndedAt             *time.Time `json:"endedAt,omitempty"`
	Outcome             Outcome    `json:"outcome,omitempty"`
	HandoffTargetUserID string     `json:"handoffTargetUserId,omitempty"`
	EndReason           string     `json:"endReason,omitempty"`
	InboundMessageCount int        `json:"inboundMessageCount"`
	AIMessageCount      int        `json:"aiMessageCount"`
	ToolCallCount       int        `json:"toolCallCount"`
	Model               string     `json:"model,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

func (s *Session) IsOpen() bool {
	return s != nil && s.EndedAt == nil
}

func (s *Session) End(outcome Outcome, reason, handoffUserID string, at time.Time) error {
	if s == nil {
		return ErrSessionNotFound
	}
	if !s.IsOpen() {
		return ErrAlreadyEnded
	}
	if !outcome.Valid() {
		outcome = OutcomeContained
	}
	s.Outcome = outcome
	s.EndReason = strings.TrimSpace(reason)
	s.HandoffTargetUserID = strings.TrimSpace(handoffUserID)
	s.EndedAt = &at
	s.UpdatedAt = at
	return nil
}

type Repository interface {
	Create(s *Session) error
	Update(s *Session) error
	FindOpenByEntry(workspaceID, entryID, entryType string) (*Session, error)
	// FindOpenByCallID finds an open voice session by SIP/CDR call id (handoff paths
	// often only know call_id, while open was keyed by campaign entry id).
	FindOpenByCallID(workspaceID, callID string) (*Session, error)
	// ExistsByCallID reports whether any session already tracks this call (backfill guard).
	ExistsByCallID(workspaceID, callID string) (bool, error)
	FindByID(id string) (*Session, error)
	ListByEntry(workspaceID, entryID, entryType string, limit, offset int) ([]*Session, int64, error)
	// Stats aggregates outcomes for a workspace in an optional time window.
	Stats(workspaceID string, from, to *time.Time) (*Stats, error)
}

type Stats struct {
	Total           int64   `json:"total"`
	Contained       int64   `json:"contained"`
	HandedOff       int64   `json:"handed_off"`
	Abandoned       int64   `json:"abandoned"`
	Error           int64   `json:"error"`
	Suppressed      int64   `json:"suppressed"`
	ContainmentRate float64 `json:"containment_rate"`
	HandoffRate     float64 `json:"handoff_rate"`
}

type StartInput struct {
	WorkspaceID string
	EntryID     string
	EntryType   string
	AgentID     string
	Channel     string
	CallID      string
	CampaignID  string
	Model       string
}

type EndInput struct {
	SessionID           string
	WorkspaceID         string
	EntryID             string
	EntryType           string
	Outcome             Outcome
	Reason              string
	HandoffTargetUserID string
}
