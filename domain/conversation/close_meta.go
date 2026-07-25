package conversation

import "time"

// CloseSource says who moved the conversation to finished.
// Status stays new|ongoing|finished only; provenance lives here.
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

// CloseReason is a stable machine code for why the conversation was finished.
type CloseReason string

const (
	CloseReasonManual       CloseReason = "manual"
	CloseReasonCustomerIdle CloseReason = "customer_idle"
	CloseReasonAIResolved   CloseReason = "ai_resolved"
	// CloseReasonMaxAge: absolute inactivity (any side) past workspace max-age.
	// Industry: LivePerson-style long inactivity hygiene (not queue abandon).
	CloseReasonMaxAge CloseReason = "max_age"
	// CloseReasonWorkflow: deterministic finish by a workflow action node.
	CloseReasonWorkflow CloseReason = "workflow"
)

func (r CloseReason) Valid() bool {
	switch r {
	case CloseReasonManual, CloseReasonCustomerIdle, CloseReasonAIResolved, CloseReasonMaxAge, CloseReasonWorkflow:
		return true
	}
	return false
}

// StatusWrite is the single shape entry repos use to apply conversation lifecycle.
// Non-finish transitions leave close columns alone unless ClearCloseMeta is set.
type StatusWrite struct {
	Status ConversationStatus
	// SetCloseMeta stamps source/reason/closed_at (finish path).
	SetCloseMeta bool
	CloseSource  CloseSource
	CloseReason  CloseReason
	ClosedAt     time.Time
	// ClearCloseMeta nulls close columns (reopen path).
	ClearCloseMeta bool
}

// AutoCloseCandidate is one open conversation past workspace idle policy.
// Returned by a single JOIN query (entries + campaigns + workspace_configs).
type AutoCloseCandidate struct {
	EntryID            string
	EntryType          string
	WorkspaceID        string
	LastAgentMessageAt time.Time
}

// DefaultAutoCloseIdleAfterHours is the workspace default when unset/zero after clamp.
const DefaultAutoCloseIdleAfterHours = 24

// Min/Max clamp for workspace auto-close idle hours.
const (
	MinAutoCloseIdleAfterHours = 1
	MaxAutoCloseIdleAfterHours = 168 // 7 days
)

// ClampAutoCloseIdleHours normalizes a configured idle window.
func ClampAutoCloseIdleHours(hours int) int {
	if hours < MinAutoCloseIdleAfterHours {
		return DefaultAutoCloseIdleAfterHours
	}
	if hours > MaxAutoCloseIdleAfterHours {
		return MaxAutoCloseIdleAfterHours
	}
	return hours
}
