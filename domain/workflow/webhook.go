package workflow

import (
	"errors"
	"strings"
	"time"
)

type WebhookAuthMode string

const (
	WebhookAuthNone        WebhookAuthMode = "none"
	WebhookAuthHeaderToken WebhookAuthMode = "header_token"
	WebhookAuthHMAC        WebhookAuthMode = "hmac"
)

func (m WebhookAuthMode) Valid() bool {
	switch m {
	case WebhookAuthNone, WebhookAuthHeaderToken, WebhookAuthHMAC:
		return true
	}
	return false
}

const (
	DefaultWebhookTokenHeader     = "X-Webhook-Token"
	DefaultWebhookSignatureHeader = "X-Signature-256"
	DefaultWebhookMethod          = "POST"
)

type WorkflowWebhook struct {
	ID          string          `json:"id"`
	WorkflowID  string          `json:"workflowId"`
	WorkspaceID string          `json:"workspaceId"`
	Token       string          `json:"token"`
	AuthMode    WebhookAuthMode `json:"authMode"`
	Secret      string          `json:"secret"`
	HeaderName  string          `json:"headerName"`
	Method      string          `json:"method"`
	Active      bool            `json:"active"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

func (wh *WorkflowWebhook) Validate() error {
	if wh.WorkflowID == "" {
		return ErrWorkflowIDRequired
	}
	if wh.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if wh.Token == "" {
		return ErrWebhookTokenRequired
	}
	if !wh.AuthMode.Valid() {
		return ErrWebhookInvalidAuthMode
	}
	if wh.AuthMode != WebhookAuthNone {
		if wh.Secret == "" {
			return ErrWebhookSecretRequired
		}
		if wh.HeaderName == "" {
			return ErrWebhookHeaderRequired
		}
	}
	return nil
}

func (wh *WorkflowWebhook) AllowsMethod(method string) bool {
	want := wh.Method
	if want == "" {
		want = DefaultWebhookMethod
	}
	return strings.EqualFold(strings.TrimSpace(method), strings.TrimSpace(want))
}

type WorkflowWebhookRepository interface {
	Create(webhook *WorkflowWebhook) error
	Update(webhook *WorkflowWebhook) error
	FindByToken(token string) (*WorkflowWebhook, error)
	FindByWorkflowID(workflowID string) (*WorkflowWebhook, error)
	Delete(workflowID string) error
}

type EntryOwnershipChecker interface {
	OwnsEntry(workspaceID, entryID, entryType string) (bool, error)
}

// EntryResolver maps a business key carried by an external webhook payload (a
// phone number) to a concrete entry in the workspace, so callers that do not
// know Vozko's internal entry_id can still trigger a workflow. Implementations
// must stay scoped to the given workspace and never resolve an entry belonging
// to another workspace. It returns ("", "", nil) when nothing matches.
type EntryResolver interface {
	ResolveByPhone(workspaceID, phone string) (entryID string, entryType string, err error)
}

var (
	ErrWebhookNotFound         = errors.New("workflow: webhook not found")
	ErrWebhookAlreadyExists    = errors.New("workflow: webhook already exists for this workflow")
	ErrWebhookTokenRequired    = errors.New("workflow: webhook token is required")
	ErrWebhookSecretRequired   = errors.New("workflow: webhook secret is required for the selected auth mode")
	ErrWebhookHeaderRequired   = errors.New("workflow: webhook header name is required for the selected auth mode")
	ErrWebhookInvalidAuthMode  = errors.New("workflow: invalid webhook auth mode")
	ErrWebhookMethodNotAllowed = errors.New("workflow: webhook does not allow this HTTP method")
	ErrWebhookUnauthorized     = errors.New("workflow: webhook authentication failed")
	ErrWebhookNoTriggerNode    = errors.New("workflow: workflow has no webhook trigger node")
	ErrWebhookEntryRequired    = errors.New("workflow: webhook payload must include entry_id and entry_type, or a phone")
	ErrWebhookEntryNotFound    = errors.New("workflow: webhook could not resolve an entry from the payload")
	ErrWebhookEntryForbidden   = errors.New("workflow: webhook entry does not belong to this workspace")
	ErrWorkspaceAtCapacity     = errors.New("workflow: workspace is at its concurrent run limit")
)
