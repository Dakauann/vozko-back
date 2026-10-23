package emailoutreach

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	es "vozko/domain/emailsending"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	"vozko/usecases/transactionalemail"
)

const (
	CodeIdempotencyKeyRequired = transactionalemail.CodeIdempotencyKeyRequired
	CodeForbidden              = "email_outreach_forbidden"

	idempotencyPrefix = "outreach:"
)

var (
	ErrIncomplete              = errors.New("emailoutreach: a required dependency is missing")
	ErrIdempotencyKeyRequired  = errors.New("emailoutreach: every compose needs its own idempotency key")
	ErrOutsideDepartmentScope  = errors.New("emailoutreach: the sender's inbox is outside the operator's departments")
	errSenderUnavailableToSend = errors.New("emailoutreach: the sender cannot be used")
)

type Senders interface {
	Get(ctx context.Context, workspaceID, id string) (*es.Sender, error)
	GetDefault(ctx context.Context, workspaceID string) (*es.Sender, error)
}

type Inboxes interface {
	FindByID(ctx context.Context, workspaceID, id string) (*em.Inbox, error)
}

type Conversations interface {
	FindByID(ctx context.Context, id string) (*em.Conversation, error)
}

type ConversationClaimer interface {
	ClaimIfUnassigned(entryID, entryType, accountID, workspaceID, userID, trigger string) (bool, error)
}

type Deps struct {
	Sender        transactionalemail.Sender
	Senders       Senders
	Inboxes       Inboxes
	Conversations Conversations
	Assignments   ConversationClaimer
}

type Input struct {
	WorkspaceID    string
	UserID         string
	IsAdmin        bool
	Departments    conversation.DepartmentAccessScope
	LeadID         string
	Address        string
	SenderID       string
	Subject        string
	Text           string
	DesignID       string
	IdempotencyKey string
}

type Started struct {
	ConversationID string
	EntryType      string
	MessageID      string
	Repeated       bool
	Recorded       bool
}

type StartConversation struct {
	d Deps
}

func NewStartConversation(d Deps) (*StartConversation, error) {
	if d.Sender == nil || d.Senders == nil || d.Inboxes == nil || d.Conversations == nil || d.Assignments == nil {
		return nil, ErrIncomplete
	}
	return &StartConversation{d: d}, nil
}

func refuse(code string, err error) error {
	return &transactionalemail.Refusal{Code: code, Err: err}
}

func (uc *StartConversation) Execute(ctx context.Context, in Input) (Started, error) {
	if err := in.check(); err != nil {
		return Started{}, err
	}
	sender, err := uc.sender(ctx, in)
	if err != nil {
		return Started{}, err
	}
	if err := uc.reaches(ctx, in, sender); err != nil {
		return Started{}, err
	}
	sent, err := uc.d.Sender.Send(ctx, transactionalemail.Request{
		WorkspaceID:    in.WorkspaceID,
		SenderID:       sender.ID,
		To:             strings.TrimSpace(in.Address),
		LeadID:         strings.TrimSpace(in.LeadID),
		Subject:        in.Subject,
		Body:           transactionalemail.Body{Text: in.Text, DesignID: strings.TrimSpace(in.DesignID)},
		IdempotencyKey: idempotencyPrefix + strings.TrimSpace(in.IdempotencyKey),
		Origin:         transactionalemail.OriginOutreach,
		SentBy:         conversation.SentByPerson(in.UserID),
	})
	if err != nil {
		return Started{}, err
	}
	started := Started{
		ConversationID: sent.ConversationID,
		EntryType:      string(shared.EntryTypeEmail),
		MessageID:      sent.MessageID,
		Repeated:       sent.Repeated,
		Recorded:       sent.ConversationID != "",
	}
	if started.Recorded {
		uc.claim(ctx, in, sent.ConversationID)
	}
	return started, nil
}

func (in Input) check() error {
	switch {
	case strings.TrimSpace(in.WorkspaceID) == "":
		return transactionalemail.ErrWorkspaceRequired
	case strings.TrimSpace(in.UserID) == "":
		return transactionalemail.ErrSentByInvalid
	case strings.TrimSpace(in.IdempotencyKey) == "":
		return refuse(CodeIdempotencyKeyRequired, ErrIdempotencyKeyRequired)
	case strings.TrimSpace(in.LeadID) == "" && strings.TrimSpace(in.Address) == "":
		return refuse(transactionalemail.CodeRecipientMissing, transactionalemail.ErrRecipientMissing)
	}
	return nil
}

func (uc *StartConversation) sender(ctx context.Context, in Input) (*es.Sender, error) {
	var (
		sender *es.Sender
		err    error
	)
	if id := strings.TrimSpace(in.SenderID); id != "" {
		sender, err = uc.d.Senders.Get(ctx, in.WorkspaceID, id)
	} else {
		sender, err = uc.d.Senders.GetDefault(ctx, in.WorkspaceID)
	}
	switch {
	case errors.Is(err, es.ErrSenderNotFound), errors.Is(err, es.ErrNoDefaultSender):
		return nil, refuse(transactionalemail.CodeSenderMissing, err)
	case err != nil:
		return nil, fmt.Errorf("emailoutreach: reading the sender: %w", err)
	case sender == nil:
		return nil, refuse(transactionalemail.CodeSenderMissing, errSenderUnavailableToSend)
	}
	return sender, nil
}

func (uc *StartConversation) reaches(ctx context.Context, in Input, sender *es.Sender) error {
	if in.IsAdmin {
		return nil
	}
	inbox, err := uc.d.Inboxes.FindByID(ctx, in.WorkspaceID, sender.ID)
	department := ""
	switch {
	case errors.Is(err, em.ErrInboxNotFound):
	case err != nil:
		return fmt.Errorf("emailoutreach: reading the inbox of sender %s: %w", sender.ID, err)
	case inbox.DepartmentID != nil:
		department = *inbox.DepartmentID
	}
	if !in.Departments.Reaches(department) {
		return refuse(CodeForbidden, ErrOutsideDepartmentScope)
	}
	return nil
}

func (uc *StartConversation) claim(ctx context.Context, in Input, conversationID string) {
	conv, err := uc.d.Conversations.FindByID(ctx, conversationID)
	if err != nil {
		log.Printf("[email-outreach] email sent into conversation %s but its mailbox could not be read to assign %s: %v", conversationID, in.UserID, err)
		return
	}
	if _, err := uc.d.Assignments.ClaimIfUnassigned(conv.ID, string(shared.EntryTypeEmail), conv.SenderID, in.WorkspaceID, in.UserID, ia.TriggerOutreachSent); err != nil {
		log.Printf("[email-outreach] email sent into conversation %s but it could not be assigned to %s: %v", conversationID, in.UserID, err)
	}
}
