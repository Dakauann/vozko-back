package emailinbox

import (
	"context"
	"errors"
	"fmt"
	"log"

	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/usecases/shared/automationrefs"
)

type ReferenceChecker interface {
	OwnedBy(ctx context.Context, workspaceID string, refs automationrefs.Refs) error
}

type Inboxes struct {
	inboxes em.InboxRepository
	senders Senders
	domains SendingDomains
	refs    ReferenceChecker
}

func NewInboxes(inboxes em.InboxRepository, senders Senders, domains SendingDomains, refs ReferenceChecker) *Inboxes {
	if inboxes == nil || senders == nil || domains == nil || refs == nil {
		panic("emailinbox: the inbox settings are missing a dependency")
	}
	return &Inboxes{inboxes: inboxes, senders: senders, domains: domains, refs: refs}
}

func (u *Inboxes) List(ctx context.Context, workspaceID string) ([]*em.Inbox, error) {
	senders, err := u.senders.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]*em.Inbox, 0, len(senders))
	for _, sender := range senders {
		inbox, err := u.ensure(ctx, sender)
		if err != nil {
			log.Printf("%s inbox of sender %s not listed: %v", logPrefix, sender.ID, err)
			continue
		}
		out = append(out, inbox)
	}
	return out, nil
}

func (u *Inboxes) Get(ctx context.Context, workspaceID, senderID string) (*em.Inbox, error) {
	sender, err := u.senders.Get(ctx, workspaceID, senderID)
	if errors.Is(err, emailsending.ErrSenderNotFound) {
		return u.inboxes.FindByID(ctx, workspaceID, senderID)
	}
	if err != nil {
		return nil, err
	}
	return u.ensure(ctx, *sender)
}

func (u *Inboxes) Update(ctx context.Context, workspaceID, senderID string, settings em.InboxSettings) (*em.Inbox, error) {
	inbox, err := u.Get(ctx, workspaceID, senderID)
	if err != nil {
		return nil, err
	}
	inbox.Apply(settings)
	if err := u.refs.OwnedBy(ctx, workspaceID, automationrefs.Refs{
		AgentID: inbox.AgentID, WorkflowID: inbox.WorkflowID, PipelineID: inbox.PipelineID, DepartmentID: inbox.DepartmentID,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", em.ErrInvalidReference, err)
	}
	if err := u.inboxes.Update(ctx, inbox); err != nil {
		return nil, err
	}
	return u.inboxes.FindByID(ctx, workspaceID, senderID)
}

func (u *Inboxes) ensure(ctx context.Context, sender emailsending.Sender) (*em.Inbox, error) {
	domain, err := u.domains.Get(ctx, sender.WorkspaceID, sender.DomainID)
	if err != nil {
		return nil, err
	}
	return u.inboxes.Ensure(ctx, em.InboxForSender(sender, *domain))
}
