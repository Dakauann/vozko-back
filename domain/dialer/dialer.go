package dialer

import "time"

type SessionStatus string

const (
	SessionStatusPending SessionStatus = "pending"
	SessionStatusDialing SessionStatus = "dialing"
	SessionStatusActive  SessionStatus = "active"
	SessionStatusEnding  SessionStatus = "ending"
	SessionStatusEnded   SessionStatus = "ended"
	SessionStatusFailed  SessionStatus = "failed"
)

type ParticipantRole string

const (
	ParticipantRoleOwner    ParticipantRole = "owner"
	ParticipantRoleObserver ParticipantRole = "observer"
)

type Session struct {
	ID                string        `json:"id"`
	WorkspaceID       string        `json:"workspaceId"`
	OwnerUserID       string        `json:"ownerUserId"`
	OwnerConnectionID string        `json:"ownerConnectionId"`
	EntryID           string        `json:"entryId"`
	EntryType         string        `json:"entryType"`
	TargetPhone       string        `json:"targetPhone"`
	Status            SessionStatus `json:"status"`
	StartedAt         *time.Time    `json:"startedAt,omitempty"`
	EndedAt           *time.Time    `json:"endedAt,omitempty"`
	EndReason         *string       `json:"endReason,omitempty"`
	TransferToUserID  *string       `json:"transferToUserId,omitempty"`
	CreatedAt         time.Time     `json:"createdAt"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}

func (s *Session) Normalize() {
	if s == nil {
		return
	}
	if s.Status == "" {
		s.Status = SessionStatusPending
	}
}

func (s *Session) Validate() error {
	if s == nil {
		return ErrSessionNotFound
	}
	if s.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if s.OwnerUserID == "" {
		return ErrOwnerRequired
	}
	if s.TargetPhone == "" {
		return ErrTargetPhoneRequired
	}
	return nil
}

type Participant struct {
	SessionID      string          `json:"sessionId"`
	UserID         string          `json:"userId"`
	ConnectionID   string          `json:"connectionId"`
	Role           ParticipantRole `json:"role"`
	JoinedAt       time.Time       `json:"joinedAt"`
	DisconnectedAt *time.Time      `json:"disconnectedAt,omitempty"`
}

type StartOutboundSessionInput struct {
	WorkspaceID       string
	OwnerUserID       string
	OwnerConnectionID string
	EntryID           string
	EntryType         string
	TargetPhone       string
	SIPTrunkID        string
	DepartmentID      string
	IsAdmin           bool
}
