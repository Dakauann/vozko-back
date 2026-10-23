package emailinbox

import (
	"context"
	"errors"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
)

var (
	ErrConversationNotFound = errors.New("emailinbox: conversation not found")
	ErrContactNotFound      = errors.New("emailinbox: contact not found")
	ErrInboxNotFound        = errors.New("emailinbox: inbox not found")
	ErrUnparseableMessage   = errors.New("emailinbox: inbound message could not be parsed")
	ErrNoSender             = errors.New("emailinbox: inbound message has no sender address")
	ErrEmptyReply           = errors.New("emailinbox: reply text is empty")
	ErrReplyTooLong         = errors.New("emailinbox: reply text is too long")
	ErrInvalidReference     = errors.New("emailinbox: referenced agent, workflow, funnel or department is not in the workspace")
)

type InboxRepository interface {
	Ensure(ctx context.Context, inbox Inbox) (*Inbox, error)
	FindByID(ctx context.Context, workspaceID, id string) (*Inbox, error)
	FindByIDUnscoped(ctx context.Context, id string) (*Inbox, error)
	FindByIDsUnscoped(ctx context.Context, ids []string) ([]*Inbox, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*Inbox, error)
	Update(ctx context.Context, inbox *Inbox) error
}

type ContactRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateContactInput) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByAddress(ctx context.Context, workspaceID string, address emailaddress.Address) (*Contact, error)
	UpdateDisplayName(ctx context.Context, id, displayName string) error
	LinkLead(ctx context.Context, id, leadID string) error
}

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateConversationInput) (*Conversation, bool, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Conversation, error)
	FindByContact(ctx context.Context, workspaceID, contactID string) (*Conversation, error)
	ApplyThread(ctx context.Context, id string, thread Thread) error
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)
	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	StatusForEntry(ctx context.Context, id string) (string, error)
	CountByStatus(ctx context.Context, workspaceID, senderID string) (map[string]int64, error)
}
