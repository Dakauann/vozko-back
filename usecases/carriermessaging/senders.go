package carriermessaging_usecase

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/rcs"
)

var (
	ErrCNPJInvalid   = errors.New("rcs: the CNPJ is not valid")
	ErrPauseNotYours = errors.New("rcs: this pause can only be lifted by the platform or the carrier")
)

type DocumentValidator interface {
	ValidateCPFOrCNPJ(document string) bool
}

type SenderInput struct {
	BrandName                string
	Description              string
	LogoURL                  string
	BannerURL                string
	Color                    string
	UseCase                  rcs.UseCase
	Sector                   rcs.Sector
	GamblingAuthorizationURL string
	ContactName              string
	ContactEmail             string
	ContactPhone             string
	WebsiteURL               string
	PrivacyURL               string
	TermsURL                 string
	CNPJ                     string
	BillingCategory          rcs.BillingCategory
}

func (in SenderInput) applyTo(s *rcs.Sender) {
	s.BrandName, s.Description = in.BrandName, in.Description
	s.LogoURL, s.BannerURL, s.Color = in.LogoURL, in.BannerURL, in.Color
	s.UseCase, s.Sector, s.GamblingAuthorizationURL = in.UseCase, in.Sector, in.GamblingAuthorizationURL
	s.ContactName, s.ContactEmail, s.ContactPhone = in.ContactName, in.ContactEmail, in.ContactPhone
	s.WebsiteURL, s.PrivacyURL, s.TermsURL, s.CNPJ = in.WebsiteURL, in.PrivacyURL, in.TermsURL, in.CNPJ
	s.BillingCategory = in.BillingCategory
	s.Normalize()
}

func (s *Service) SuggestedDescription(sender rcs.Sender) string {
	return rcs.ISVDescription(s.PlatformName, sender.BrandName, sender.UseCase)
}

func (s *Service) CreateSender(ctx context.Context, workspaceID string, in SenderInput) (*rcs.Sender, error) {
	sender := rcs.Sender{WorkspaceID: workspaceID, Status: rcs.SenderDraft, Provider: s.Provider.Name()}
	in.applyTo(&sender)
	if sender.BrandName == "" {
		return nil, &rcs.IncompleteError{Field: "brand_name"}
	}
	if sender.Description == "" {
		sender.Description = s.SuggestedDescription(sender)
	}
	now := s.Now()
	sender.CreatedAt, sender.UpdatedAt = now, now
	if err := s.Repos.Senders.Create(ctx, &sender); err != nil {
		return nil, err
	}
	return &sender, nil
}

func (s *Service) UpdateSender(ctx context.Context, workspaceID, id string, in SenderInput) (*rcs.Sender, error) {
	sender, err := s.Repos.Senders.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if !sender.Editable() {
		return nil, rcs.ErrSenderTransition
	}
	in.applyTo(sender)
	if sender.BrandName == "" {
		return nil, &rcs.IncompleteError{Field: "brand_name"}
	}
	sender.UpdatedAt = s.Now()
	if err := s.Repos.Senders.Update(ctx, sender); err != nil {
		return nil, err
	}
	return sender, nil
}

func (s *Service) SubmitSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error) {
	sender, err := s.Repos.Senders.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	sender.PlatformEmailDomain = s.PlatformEmailDomain
	submitted, err := sender.Submit(s.Now())
	if err != nil {
		return nil, err
	}
	if !s.Documents.ValidateCPFOrCNPJ(submitted.CNPJ) {
		return nil, ErrCNPJInvalid
	}
	registration, err := s.Provider.SubmitSender(ctx, submitted)
	if err != nil {
		return nil, err
	}
	submitted.Provider = s.Provider.Name()
	submitted.ProviderRequestID, submitted.ProviderProfileID = registration.RequestID, registration.ProfileID
	submitted.UpdatedAt = s.Now()
	if err := s.Repos.Senders.Update(ctx, &submitted); err != nil {
		return nil, err
	}
	s.Notices.SenderStateChanged(ctx, submitted)
	return &submitted, nil
}

func (s *Service) PauseSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error) {
	sender, err := s.Repos.Senders.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return s.pause(ctx, *sender, rcs.PauseManual)
}

func (s *Service) AdminPauseSender(ctx context.Context, id string, reason rcs.PauseReason) (*rcs.Sender, error) {
	sender, err := s.Repos.Senders.FindByIDUnscoped(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.pause(ctx, *sender, reason)
}

func (s *Service) pause(ctx context.Context, sender rcs.Sender, reason rcs.PauseReason) (*rcs.Sender, error) {
	paused, err := sender.Pause(rcs.PausedByPlatform, reason, s.Now())
	if err != nil {
		return nil, err
	}
	return s.saveState(ctx, paused)
}

func (s *Service) ResumeSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error) {
	sender, err := s.Repos.Senders.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if sender.Status == rcs.SenderPaused && !sender.ResumableByWorkspace() {
		return nil, ErrPauseNotYours
	}
	return s.resume(ctx, *sender)
}

func (s *Service) AdminResumeSender(ctx context.Context, id string) (*rcs.Sender, error) {
	sender, err := s.Repos.Senders.FindByIDUnscoped(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.resume(ctx, *sender)
}

func (s *Service) resume(ctx context.Context, sender rcs.Sender) (*rcs.Sender, error) {
	resumed, err := sender.Resume(s.Now())
	if err != nil {
		return nil, err
	}
	return s.saveState(ctx, resumed)
}

func (s *Service) saveState(ctx context.Context, sender rcs.Sender) (*rcs.Sender, error) {
	sender.UpdatedAt = s.Now()
	if err := s.Repos.Senders.Update(ctx, &sender); err != nil {
		return nil, err
	}
	s.Notices.SenderStateChanged(ctx, sender)
	return &sender, nil
}

func (s *Service) DeleteSender(ctx context.Context, workspaceID, id string) error {
	sender, err := s.Repos.Senders.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if !sender.Editable() {
		return rcs.ErrSenderTransition
	}
	return s.Repos.Senders.Delete(ctx, workspaceID, id)
}

func (s *Service) ListSenders(ctx context.Context, workspaceID string) ([]*rcs.Sender, error) {
	return s.Repos.Senders.ListByWorkspace(ctx, workspaceID)
}

func (s *Service) GetSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error) {
	return s.Repos.Senders.FindByID(ctx, workspaceID, id)
}

func (s *Service) SyncSenders(ctx context.Context, limit int) (int, error) {
	senders, err := s.Repos.Senders.ListAwaitingProvider(ctx, limit)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, sender := range senders {
		report, err := s.Provider.SenderStatus(ctx, *sender)
		if err != nil {
			log.Printf("[rcs] sender %s status: %v", sender.ID, err)
			continue
		}
		next := sender.ApplyProviderState(report.State, report.Reason, s.Now())
		if next.Status == sender.Status && next.StatusReason == strings.TrimSpace(sender.StatusReason) && next.PausedBy == sender.PausedBy {
			continue
		}
		if _, err := s.saveState(ctx, next); err != nil {
			log.Printf("[rcs] sender %s save: %v", sender.ID, err)
			continue
		}
		changed++
	}
	return changed, nil
}

func (s *Service) AdminListSenders(ctx context.Context, status *rcs.SenderStatus) ([]*rcs.Sender, error) {
	return s.Repos.Senders.ListAll(ctx, status)
}
