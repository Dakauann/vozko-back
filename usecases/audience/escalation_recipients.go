package audience_usecase

import (
	"context"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
)

// Who a comment can be forwarded to (§3).
//
// The plan left "who is the confiado" open. This is the answer, and the reason
// it is the safe one: the recipient is someone the workspace ALREADY has an
// open conversation with. So forwarding cannot become a way to cold-message a
// stranger from the comment dashboard, no template is bought, no 24-hour
// window has to be reasoned about, and the message lands on the channel that
// person already answers on.
//
// The port is declared here, by the consumer, and named for the question it
// answers rather than for the table behind it. The composition root binds it to
// the inbox search the CRM already runs.

// EscalationRecipient is one conversation a comment can be forwarded into.
type EscalationRecipient struct {
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
	Name      string `json:"name,omitempty"`
	Number    string `json:"number,omitempty"`
	// WindowOpen says whether a free-form message can reach them right now.
	// Shown rather than enforced: the channels differ, and the send path is
	// the authority on its own rules.
	WindowOpen    bool   `json:"windowOpen"`
	LastMessageAt string `json:"lastMessageAt,omitempty"`
}

// EscalationRecipientQuery is a workspace-wide search over that user's own
// reachable conversations. Deliberately not campaign-scoped: someone forwarding
// a hostile comment to their manager should not have to know which campaign
// their manager is filed under.
type EscalationRecipientQuery struct {
	WorkspaceID string
	UserID      string
	Query       string
	Limit       int
}

// EscalationRecipientLister answers "which conversations may I forward into".
type EscalationRecipientLister interface {
	ListEscalationRecipients(ctx context.Context, in EscalationRecipientQuery) ([]EscalationRecipient, error)
}

// ListEscalationRecipientsUseCase is the read behind the recipient picker.
type ListEscalationRecipientsUseCase interface {
	Execute(ctx context.Context, in EscalationRecipientQuery) ([]EscalationRecipient, error)
}

const (
	defaultRecipientLimit = 20
	maxRecipientLimit     = 50
)

type listEscalationRecipientsUseCase struct{ lister EscalationRecipientLister }

func NewListEscalationRecipientsUseCase(lister EscalationRecipientLister) ListEscalationRecipientsUseCase {
	return &listEscalationRecipientsUseCase{lister: lister}
}

func (uc *listEscalationRecipientsUseCase) Execute(ctx context.Context, in EscalationRecipientQuery) ([]EscalationRecipient, error) {
	if uc.lister == nil {
		return nil, fmt.Errorf("%w: conversations are not available in this deployment", ca.ErrInvalidFilter)
	}
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	if in.WorkspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ca.ErrInvalidFilter)
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Limit <= 0 {
		in.Limit = defaultRecipientLimit
	}
	if in.Limit > maxRecipientLimit {
		in.Limit = maxRecipientLimit
	}
	rows, err := uc.lister.ListEscalationRecipients(ctx, in)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []EscalationRecipient{}
	}
	return rows, nil
}
