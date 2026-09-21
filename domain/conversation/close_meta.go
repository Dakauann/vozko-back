package conversation

import "time"

type CloseSource string

const (
	CloseSourceHuman  CloseSource = "human"
	CloseSourceAI     CloseSource = "ai"
	CloseSourceSystem CloseSource = "system"
)

func (s CloseSource) Valid() bool {
	switch s {
	case CloseSourceHuman, CloseSourceAI, CloseSourceSystem:
		return true
	}
	return false
}

type CloseReason string

const (
	CloseReasonManual       CloseReason = "manual"
	CloseReasonCustomerIdle CloseReason = "customer_idle"
	CloseReasonAIResolved   CloseReason = "ai_resolved"
	CloseReasonMaxAge       CloseReason = "max_age"
	CloseReasonWorkflow     CloseReason = "workflow"
)

func (r CloseReason) Valid() bool {
	switch r {
	case CloseReasonManual, CloseReasonCustomerIdle, CloseReasonAIResolved, CloseReasonMaxAge, CloseReasonWorkflow:
		return true
	}
	return false
}

type StatusWrite struct {
	Status         ConversationStatus
	SetCloseMeta   bool
	CloseSource    CloseSource
	CloseReason    CloseReason
	ClosedAt       time.Time
	ClearCloseMeta bool
}

type AutoCloseCandidate struct {
	EntryID            string
	EntryType          string
	WorkspaceID        string
	LastAgentMessageAt time.Time
}

const DefaultAutoCloseIdleAfterHours = 24

const (
	MinAutoCloseIdleAfterHours = 1
	MaxAutoCloseIdleAfterHours = 168
)

func ClampAutoCloseIdleHours(hours int) int {
	if hours < MinAutoCloseIdleAfterHours {
		return DefaultAutoCloseIdleAfterHours
	}
	if hours > MaxAutoCloseIdleAfterHours {
		return MaxAutoCloseIdleAfterHours
	}
	return hours
}
