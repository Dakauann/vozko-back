package emailsending

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	es "vozko/domain/emailsending"
)

const (
	defaultLocalPart     = "contato"
	maxDefaultNameLength = 80
)

type Senders struct {
	c *core
}

type SenderView struct {
	Sender  es.Sender
	Address string
	Domain  es.SendingDomain
}

type CreateSenderInput struct {
	DomainID       string
	LocalPart      string
	DisplayName    string
	ReplyForwardTo string
	IsDefault      bool
}

type UpdateSenderInput struct {
	DomainID       *string
	LocalPart      *string
	DisplayName    *string
	ReplyForwardTo *string
	IsDefault      *bool
}

func displayNameFrom(name, fallback string) string {
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune("\r\n\x00\"<>", r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	cleaned = strings.TrimSpace(cleaned)
	if utf8.RuneCountInString(cleaned) > maxDefaultNameLength {
		cleaned = strings.TrimSpace(string([]rune(cleaned)[:maxDefaultNameLength]))
	}
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

func senderViewOf(sender es.Sender, d es.SendingDomain) SenderView {
	return SenderView{Sender: sender, Address: string(sender.FromAddress(d)), Domain: d}
}

func senderViews(senders []es.Sender, domains []es.SendingDomain) ([]SenderView, error) {
	byID := make(map[string]es.SendingDomain, len(domains))
	for _, d := range domains {
		byID[d.ID] = d
	}
	out := make([]SenderView, 0, len(senders))
	for _, sender := range senders {
		d, ok := byID[sender.DomainID]
		if !ok {
			return nil, es.ErrDomainNotFound
		}
		out = append(out, senderViewOf(sender, d))
	}
	return out, nil
}

func (s *Senders) List(ctx context.Context, workspaceID string) ([]SenderView, error) {
	senders, err := s.c.Senders.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	domains, err := s.c.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return senderViews(senders, domains)
}

func (s *Senders) Create(ctx context.Context, workspaceID string, in CreateSenderInput) (SenderView, error) {
	d, err := s.c.Domains.Get(ctx, workspaceID, in.DomainID)
	if err != nil {
		return SenderView{}, err
	}
	sender, err := es.NewSender(*d, in.LocalPart, in.DisplayName, in.ReplyForwardTo)
	if err != nil {
		return SenderView{}, err
	}
	sender.IsDefault = in.IsDefault
	if !sender.IsDefault {
		if _, err := s.c.Senders.GetDefault(ctx, workspaceID); errors.Is(err, es.ErrSenderNotFound) {
			sender.IsDefault = true
		} else if err != nil {
			return SenderView{}, err
		}
	}
	if err := s.c.Senders.Create(ctx, &sender); err != nil {
		return SenderView{}, err
	}
	return senderViewOf(sender, *d), nil
}

func (s *Senders) Update(ctx context.Context, workspaceID, id string, in UpdateSenderInput) (SenderView, error) {
	current, err := s.c.Senders.Get(ctx, workspaceID, id)
	if err != nil {
		return SenderView{}, err
	}
	domainID, local, name, forward := current.DomainID, current.LocalPart, current.DisplayName, string(current.ReplyForwardTo)
	if in.DomainID != nil {
		domainID = *in.DomainID
	}
	if in.LocalPart != nil {
		local = *in.LocalPart
	}
	if in.DisplayName != nil {
		name = *in.DisplayName
	}
	if in.ReplyForwardTo != nil {
		forward = *in.ReplyForwardTo
	}
	d, err := s.c.Domains.Get(ctx, workspaceID, domainID)
	if err != nil {
		return SenderView{}, err
	}
	next, err := es.NewSender(*d, local, name, forward)
	if err != nil {
		return SenderView{}, err
	}
	next.ID, next.CreatedAt, next.IsDefault = current.ID, current.CreatedAt, current.IsDefault
	if in.IsDefault != nil {
		if !*in.IsDefault && current.IsDefault {
			return SenderView{}, ErrDefaultSenderRequired
		}
		next.IsDefault = *in.IsDefault || current.IsDefault
	}
	if err := s.c.Senders.Update(ctx, &next); err != nil {
		return SenderView{}, err
	}
	return senderViewOf(next, *d), nil
}

func (s *Senders) Delete(ctx context.Context, workspaceID, id string) error {
	current, err := s.c.Senders.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if current.IsDefault {
		all, err := s.c.Senders.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		if len(all) > 1 {
			return ErrDefaultSenderRequired
		}
	}
	return s.c.Senders.Delete(ctx, workspaceID, id)
}
