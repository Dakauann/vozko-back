package audience_usecase

import (
	"context"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
)

type EscalationRecipient struct {
	EntryID       string `json:"entryId"`
	EntryType     string `json:"entryType"`
	Name          string `json:"name,omitempty"`
	Number        string `json:"number,omitempty"`
	WindowOpen    bool   `json:"windowOpen"`
	LastMessageAt string `json:"lastMessageAt,omitempty"`
}

type EscalationRecipientQuery struct {
	WorkspaceID string
	UserID      string
	Query       string
	Limit       int
}

type EscalationRecipientLister interface {
	ListEscalationRecipients(ctx context.Context, in EscalationRecipientQuery) ([]EscalationRecipient, error)
}

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
