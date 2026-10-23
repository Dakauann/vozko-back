package readiness_usecase

import (
	"context"
	"errors"

	es "vozko/domain/emailsending"
	"vozko/domain/readiness"
	"vozko/domain/workspace"
)

type EmailAccounts interface {
	GetByWorkspace(ctx context.Context, workspaceID string) (*es.SenderAccount, error)
}

type EmailSenders interface {
	ListByWorkspace(ctx context.Context, workspaceID string) ([]es.Sender, error)
}

type EmailDomains interface {
	ListByWorkspace(ctx context.Context, workspaceID string) ([]es.SendingDomain, error)
}

type EmailProbeDeps struct {
	Accounts EmailAccounts
	Senders  EmailSenders
	Domains  EmailDomains
	Access   workspace.CheckAccessUseCase
}

type emailProbe struct{ deps EmailProbeDeps }

func NewEmailProbe(deps EmailProbeDeps) readiness.Probe { return &emailProbe{deps: deps} }

func (p *emailProbe) Capability() readiness.Capability { return readiness.Email }

func (p *emailProbe) Probe(ctx context.Context, person readiness.Person) (readiness.Status, error) {
	count, err := p.readySenders(ctx, person.WorkspaceID)
	if err != nil {
		return readiness.Status{}, err
	}
	permitted := person.SystemAdmin || (p.deps.Access != nil &&
		p.deps.Access.Execute(person.UserID, person.WorkspaceID, workspace.ResourceEmailMarketing, workspace.ActionConfigure) == nil)
	return addable(readiness.Status{Capability: readiness.Email, Count: count}, permitted, true), nil
}

func (p *emailProbe) readySenders(ctx context.Context, workspaceID string) (int, error) {
	account, err := p.deps.Accounts.GetByWorkspace(ctx, workspaceID)
	if errors.Is(err, es.ErrAccountNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if account == nil || account.CanSendOnDemand() != nil {
		return 0, nil
	}
	domains, err := p.deps.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	byID := make(map[string]es.SendingDomain, len(domains))
	for _, d := range domains {
		byID[d.ID] = d
	}
	senders, err := p.deps.Senders.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	ready := 0
	for _, s := range senders {
		if d, ok := byID[s.DomainID]; ok && s.CanSendFrom(d) == nil {
			ready++
		}
	}
	return ready, nil
}
