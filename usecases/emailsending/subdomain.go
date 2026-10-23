package emailsending

import (
	"context"
	"errors"
	"fmt"

	es "vozko/domain/emailsending"
)

type SubdomainView struct {
	Slug    string
	Address string
	Locked  bool
}

type SubdomainCheck struct {
	Slug    string
	Address string
	Refusal error
}

func (s *Setup) CheckSubdomain(ctx context.Context, workspaceID, slug string) (SubdomainCheck, error) {
	c := s.c
	out := SubdomainCheck{Slug: slug}
	if err := es.ValidateChosenSlug(slug); err != nil {
		out.Refusal = err
		return out, nil
	}
	current, kind := "", es.DomainSharedProbation
	acct, err := c.account(ctx, workspaceID)
	switch {
	case errors.Is(err, es.ErrAccountNotProvisioned):
	case err != nil:
		return SubdomainCheck{}, err
	default:
		current, kind = acct.Slug, c.sharedKindOf(acct)
	}
	if out.Address, err = c.defaultAddress(slug, kind); err != nil {
		return SubdomainCheck{}, err
	}
	free, err := c.slugFree(ctx, workspaceID, current, slug)
	if err != nil {
		return SubdomainCheck{}, err
	}
	if !free {
		out.Refusal = es.ErrSlugTaken
	}
	return out, nil
}

func (s *Setup) ChangeSubdomain(ctx context.Context, workspaceID, slug string) (StatusView, error) {
	if err := s.c.setupAllowed(workspaceID); err != nil {
		return StatusView{}, err
	}
	acct, err := s.c.account(ctx, workspaceID)
	if err != nil {
		return StatusView{}, err
	}
	if err := s.c.changeSlug(ctx, acct, slug); err != nil {
		return StatusView{}, err
	}
	return s.startWorkspaceSetup(ctx, acct)
}

func (s *Setup) startWorkspaceSetup(ctx context.Context, acct *es.SenderAccount) (StatusView, error) {
	c := s.c
	if _, err := c.settleSharedDomain(ctx, acct); err != nil {
		return StatusView{}, err
	}
	pending, err := c.domainsAwaitingSetup(ctx, acct.WorkspaceID)
	if err != nil {
		return StatusView{}, err
	}
	if err := c.startSetup(ctx, acct, pending...); err != nil {
		return StatusView{}, err
	}
	return s.Status(ctx, acct.WorkspaceID)
}

func (c *core) subdomainView(ctx context.Context, acct *es.SenderAccount) (SubdomainView, error) {
	address, err := c.defaultAddress(acct.Slug, c.sharedKindOf(acct))
	if err != nil {
		return SubdomainView{}, err
	}
	locked, err := c.Activity.SentFromSharedDomains(ctx, acct.WorkspaceID)
	if err != nil {
		return SubdomainView{}, err
	}
	return SubdomainView{Slug: acct.Slug, Address: address, Locked: locked}, nil
}

func (c *core) suggestedSubdomain(ctx context.Context, workspaceID string) (SubdomainView, error) {
	slug, err := c.suggestSlug(ctx, workspaceID)
	if err != nil {
		return SubdomainView{}, err
	}
	address, err := c.defaultAddress(slug, es.DomainSharedProbation)
	if err != nil {
		return SubdomainView{}, err
	}
	return SubdomainView{Slug: slug, Address: address}, nil
}

func (c *core) defaultAddress(slug string, kind es.DomainKind) (string, error) {
	fqdn, err := es.SharedSubdomain(slug, c.sharedDomainFor(kind))
	if err != nil {
		return "", err
	}
	return defaultLocalPart + "@" + fqdn, nil
}

func (c *core) suggestSlug(ctx context.Context, workspaceID string) (string, error) {
	name, err := c.Workspaces.WorkspaceName(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("emailsending: workspace name: %w", err)
	}
	var lookupErr error
	slug := es.SlugFor(name, workspaceID, func(candidate string) bool {
		if lookupErr != nil {
			return false
		}
		free, err := c.slugFree(ctx, workspaceID, "", candidate)
		lookupErr = err
		return err == nil && !free
	})
	return slug, lookupErr
}

func (c *core) chosenSlug(ctx context.Context, workspaceID, current, slug string) error {
	if err := es.ValidateChosenSlug(slug); err != nil {
		return err
	}
	free, err := c.slugFree(ctx, workspaceID, current, slug)
	if err != nil {
		return err
	}
	if !free {
		return es.ErrSlugTaken
	}
	return nil
}

func (c *core) slugFree(ctx context.Context, workspaceID, current, slug string) (bool, error) {
	if slug == current {
		return true, nil
	}
	taken, err := c.Accounts.SlugTaken(ctx, slug)
	if err != nil || taken {
		return false, err
	}
	for _, kind := range []es.DomainKind{es.DomainSharedProbation, es.DomainSharedTrusted} {
		fqdn, err := es.SharedSubdomain(slug, c.sharedDomainFor(kind))
		if err != nil {
			return false, err
		}
		d, err := c.Domains.GetByDomain(ctx, fqdn)
		switch {
		case errors.Is(err, es.ErrDomainNotFound):
		case err != nil:
			return false, err
		case d.WorkspaceID != workspaceID:
			return false, nil
		}
	}
	return true, nil
}

func (c *core) changeSlug(ctx context.Context, acct *es.SenderAccount, slug string) error {
	if slug == acct.Slug {
		return nil
	}
	if err := es.ValidateChosenSlug(slug); err != nil {
		return err
	}
	sent, err := c.Activity.SentFromSharedDomains(ctx, acct.WorkspaceID)
	if err != nil {
		return err
	}
	if sent {
		return es.ErrSubdomainLocked
	}
	if err := c.chosenSlug(ctx, acct.WorkspaceID, acct.Slug, slug); err != nil {
		return err
	}
	if err := c.Accounts.ChangeSlug(ctx, acct.ID, slug); err != nil {
		return err
	}
	acct.Slug = slug
	return nil
}

func (c *core) settleSharedDomain(ctx context.Context, acct *es.SenderAccount) (*es.SendingDomain, error) {
	current, err := c.sharedDomainRow(ctx, acct, c.sharedKindOf(acct))
	if err != nil {
		return nil, err
	}
	if current.ReleaseState != es.ReleaseNone {
		current.Reinstate()
		if err := c.Domains.SetRelease(ctx, current); err != nil {
			return nil, err
		}
		if err := c.Domains.Update(ctx, current); err != nil {
			return nil, err
		}
	}
	domains, err := c.Domains.ListByWorkspace(ctx, acct.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		old := &domains[i]
		named, err := c.namedAfterSlug(old, acct.Slug)
		if err != nil {
			return nil, err
		}
		if old.ID == current.ID || !old.Kind.Shared() || old.ReleaseState != es.ReleaseNone || named {
			continue
		}
		if !old.Retired {
			if err := c.Senders.MoveDomain(ctx, acct.WorkspaceID, old.ID, current.ID); err != nil {
				return nil, err
			}
		}
		old.Release()
		if err := c.Domains.SetRelease(ctx, old); err != nil {
			return nil, err
		}
	}
	return current, nil
}

func (c *core) namedAfterSlug(d *es.SendingDomain, slug string) (bool, error) {
	if !d.Kind.Shared() {
		return false, nil
	}
	fqdn, err := es.SharedSubdomain(slug, c.sharedDomainFor(d.Kind))
	if err != nil {
		return false, err
	}
	return d.Domain == fqdn, nil
}

func (c *core) releaseDomain(ctx context.Context, acct *es.SenderAccount, d *es.SendingDomain) error {
	if err := c.dropProviderSetup(ctx, acct, d); err != nil {
		return err
	}
	d.Released()
	record, cancel := outcomeContext(ctx)
	defer cancel()
	return c.Domains.SetRelease(record, d)
}

func (c *core) dropProviderSetup(ctx context.Context, acct *es.SenderAccount, d *es.SendingDomain) error {
	if err := c.Routes.RemoveReplyRoute(ctx, d.ReplyDomain); err != nil {
		return providerFailure("inbound", err)
	}
	if d.Kind.Shared() {
		if err := c.DNS.DeleteRecords(ctx, c.sharedDomainFor(d.Kind), d.Records); err != nil {
			return providerFailure("dns", err)
		}
	}
	if err := c.Provider.RemoveIdentity(ctx, acct.TenantName, d.Domain); err != nil {
		return providerFailure("identity", err)
	}
	return nil
}
