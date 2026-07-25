package cdr

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/shared"
)

type CallType string

const (
	CallTypeCRM           CallType = "crm"
	CallTypeTrunkInbound  CallType = "trunk_inbound"
	CallTypeTrunkOutbound CallType = "trunk_outbound"
)

type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

type Source string

const (
	SourceSIP           Source = "sip"
	SourceWebSocket     Source = "websocket"
	SourceWhatsApp      Source = "whatsapp"
	SourceBrowserDirect Source = "browser_direct"
)

type Status string

const (
	StatusInitiated  Status = "initiated"
	StatusRinging    Status = "ringing"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusAbandoned  Status = "abandoned"
)

type EndReason string

const (
	EndReasonHangupCaller EndReason = "hangup_caller"
	EndReasonHangupCallee EndReason = "hangup_callee"
	EndReasonEndCallTool  EndReason = "end_call_tool"
	EndReasonError        EndReason = "error"
	EndReasonTimeout      EndReason = "timeout"
	EndReasonSurrendered  EndReason = "surrendered"
)

var (
	ErrCallNotFound          = errors.New("call record not found")
	ErrCallIDRequired        = errors.New("call id is required")
	ErrWorkspaceIDRequired   = errors.New("workspace id is required")
	ErrCallTypeRequired      = errors.New("call type is required")
	ErrCallDirectionRequired = errors.New("call direction is required")
	ErrCallSourceRequired    = errors.New("call source is required")
	ErrAlreadyCompleted      = errors.New("call record already completed")
)

type Call struct {
	ID             string
	CallID         string
	WorkspaceID    string
	Type           CallType
	Direction      Direction
	Source         Source
	Status         Status
	ConversationID *string
	LeadID         *string
	PhoneFrom      string
	PhoneTo        string
	TrunkID        *string
	// AgentID is the dialer member who owned the leg (member metrics / filters).
	AgentID      *string
	ParentCallID *string
	EndReason    *string
	StartedAt    time.Time
	AnsweredAt   *time.Time
	EndedAt      *time.Time
	DurationSec  int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (c *Call) Validate() error {
	if strings.TrimSpace(c.CallID) == "" {
		return ErrCallIDRequired
	}
	if strings.TrimSpace(c.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if c.Type == "" {
		return ErrCallTypeRequired
	}
	if c.Direction == "" {
		return ErrCallDirectionRequired
	}
	if c.Source == "" {
		return ErrCallSourceRequired
	}
	return nil
}

func (c *Call) IsTerminal() bool {
	switch c.Status {
	case StatusCompleted, StatusFailed, StatusAbandoned:
		return true
	}
	return false
}

type ListFilters struct {
	WorkspaceID string
	Type        *CallType
	Direction   *Direction
	Source      *Source
	Status      *Status
	AgentID     *string
	LeadID      *string
	StartedFrom *time.Time
	StartedTo   *time.Time
	shared.QueryOptions
}
