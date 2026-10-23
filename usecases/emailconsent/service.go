package emailconsent_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailconsent"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
)

var ErrIncomplete = errors.New("emailconsent: a required dependency is missing")

type MessageStore interface {
	Get(ctx context.Context, id string) (*emailsending.Message, error)
	Update(ctx context.Context, m *emailsending.Message) error
}

type TokenOpener interface {
	Open(purpose emailtoken.Purpose, token string) (string, error)
}

type LeadFinder interface {
	FindByEmail(workspaceID, email string) (*lead.Lead, error)
}

type Deps struct {
	Subscriptions emailconsent.Repository
	Messages      MessageStore
	Tokens        TokenOpener
	Leads         LeadFinder
	Now           func() time.Time
}

type Service struct {
	deps Deps
}

func New(deps Deps) (*Service, error) {
	if deps.Subscriptions == nil || deps.Messages == nil || deps.Tokens == nil || deps.Leads == nil {
		return nil, ErrIncomplete
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Service{deps: deps}, nil
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

type SubscribeInput struct {
	Address     string
	LeadID      string
	Source      emailconsent.Source
	IP          string
	ConsentedAt *time.Time
}

func (s *Service) Subscribe(ctx context.Context, workspaceID string, in SubscribeInput) (emailconsent.Subscription, error) {
	now := s.now()
	at := now
	if in.ConsentedAt != nil {
		at = in.ConsentedAt.UTC()
	}
	consent := emailconsent.Consent{WorkspaceID: workspaceID, Address: in.Address, LeadID: strings.TrimSpace(in.LeadID), Source: in.Source, IP: in.IP, At: at}
	fresh, err := emailconsent.NewSubscription(consent, now)
	if err != nil {
		return emailconsent.Subscription{}, err
	}
	if consent.LeadID == "" {
		if consent.LeadID, err = s.leadFor(workspaceID, fresh.Address); err != nil {
			return emailconsent.Subscription{}, err
		}
		fresh.LeadID = consent.LeadID
	}
	current, err := s.deps.Subscriptions.Get(ctx, fresh.WorkspaceID, fresh.Address)
	switch {
	case errors.Is(err, emailconsent.ErrSubscriptionNotFound):
		return s.deps.Subscriptions.Save(ctx, fresh)
	case err != nil:
		return emailconsent.Subscription{}, err
	}
	renewed, changed, err := current.Renew(consent, now)
	if err != nil || !changed {
		return renewed, err
	}
	return s.deps.Subscriptions.Save(ctx, renewed)
}

func (s *Service) leadFor(workspaceID string, address emailaddress.Address) (string, error) {
	found, err := s.deps.Leads.FindByEmail(workspaceID, address.String())
	if errors.Is(err, lead.ErrLeadNotFound) || errors.Is(err, lead.ErrLeadInvalid) {
		return "", nil
	}
	if err != nil || found == nil {
		return "", err
	}
	return found.ID, nil
}

type ImportRow struct {
	Address     string
	LeadID      string
	IP          string
	ConsentedAt time.Time
}

type RowError struct {
	Row  int    `json:"row"`
	Code string `json:"code"`
}

type ImportResult struct {
	Subscribed       int        `json:"subscribed"`
	Unchanged        int        `json:"unchanged"`
	KeptUnsubscribed int        `json:"kept_unsubscribed"`
	Duplicates       int        `json:"duplicates"`
	Invalid          []RowError `json:"invalid"`
}

type importCandidate struct {
	consent emailconsent.Consent
	fresh   emailconsent.Subscription
}

func (s *Service) ImportConsent(ctx context.Context, workspaceID string, rows []ImportRow) (ImportResult, error) {
	switch {
	case len(rows) == 0:
		return ImportResult{}, emailconsent.ErrImportEmpty
	case len(rows) > emailconsent.MaxImportRows:
		return ImportResult{}, emailconsent.ErrImportTooLarge
	}
	now := s.now()
	result := ImportResult{Invalid: []RowError{}}
	seen := make(map[emailaddress.Address]bool, len(rows))
	candidates := make([]importCandidate, 0, len(rows))
	addresses := make([]emailaddress.Address, 0, len(rows))
	for i, row := range rows {
		consent := emailconsent.Consent{WorkspaceID: workspaceID, Address: row.Address, LeadID: row.LeadID, Source: emailconsent.SourceImport, IP: row.IP, At: row.ConsentedAt}
		fresh, err := emailconsent.NewSubscription(consent, now)
		if err != nil {
			if code := emailconsent.ErrorCode(err); code != "" && !errors.Is(err, emailconsent.ErrWorkspaceRequired) {
				result.Invalid = append(result.Invalid, RowError{Row: i + 1, Code: code})
				continue
			}
			return ImportResult{}, err
		}
		if seen[fresh.Address] {
			result.Duplicates++
			continue
		}
		seen[fresh.Address] = true
		candidates = append(candidates, importCandidate{consent: consent, fresh: fresh})
		addresses = append(addresses, fresh.Address)
	}
	existing, err := s.deps.Subscriptions.Find(ctx, workspaceID, addresses)
	if err != nil {
		return ImportResult{}, err
	}
	changed := make([]emailconsent.Subscription, 0, len(candidates))
	for _, c := range candidates {
		current, ok := existing[c.fresh.Address]
		if !ok {
			changed = append(changed, c.fresh)
			result.Subscribed++
			continue
		}
		renewed, moved, err := current.Renew(c.consent, now)
		if err != nil {
			return ImportResult{}, err
		}
		switch {
		case moved:
			changed = append(changed, renewed)
			result.Unchanged++
		case current.Status == emailconsent.StatusUnsubscribed:
			result.KeptUnsubscribed++
		default:
			result.Unchanged++
		}
	}
	if err := s.deps.Subscriptions.SaveMany(ctx, changed); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func (s *Service) UnsubscribeAddress(ctx context.Context, workspaceID, rawAddress string) (emailconsent.Subscription, error) {
	address, err := emailaddress.Normalize(rawAddress)
	if err != nil {
		return emailconsent.Subscription{}, emailconsent.ErrAddressInvalid
	}
	return s.unsubscribe(ctx, workspaceID, address, emailconsent.UnsubscribeOperator)
}

func (s *Service) CheckToken(token string) error {
	if _, err := s.deps.Tokens.Open(emailtoken.PurposeUnsubscribe, token); err != nil {
		return emailconsent.ErrUnsubscribeTokenInvalid
	}
	return nil
}

type UnsubscribeResult struct {
	WorkspaceID string
	Address     emailaddress.Address
}

func (s *Service) Unsubscribe(ctx context.Context, token string, source emailconsent.UnsubscribeSource) (UnsubscribeResult, error) {
	if source != emailconsent.UnsubscribeOneClick && source != emailconsent.UnsubscribeLink {
		return UnsubscribeResult{}, emailconsent.ErrUnsubscribeSourceInvalid
	}
	messageID, err := s.deps.Tokens.Open(emailtoken.PurposeUnsubscribe, token)
	if err != nil {
		return UnsubscribeResult{}, emailconsent.ErrUnsubscribeTokenInvalid
	}
	return s.unsubscribeMessage(ctx, messageID, source)
}

func (s *Service) UnsubscribeMailto(ctx context.Context, messageID string) error {
	_, err := s.unsubscribeMessage(ctx, messageID, emailconsent.UnsubscribeMailto)
	return err
}

func (s *Service) UnsubscribeByComplaint(ctx context.Context, workspaceID string, address emailaddress.Address, _ string) error {
	_, err := s.unsubscribe(ctx, workspaceID, address, emailconsent.UnsubscribeComplaint)
	return err
}

func (s *Service) unsubscribeMessage(ctx context.Context, messageID string, source emailconsent.UnsubscribeSource) (UnsubscribeResult, error) {
	msg, err := s.deps.Messages.Get(ctx, messageID)
	if errors.Is(err, emailsending.ErrMessageNotFound) {
		return UnsubscribeResult{}, emailconsent.ErrUnsubscribeTokenInvalid
	}
	if err != nil {
		return UnsubscribeResult{}, err
	}
	if msg == nil || msg.WorkspaceID == "" || msg.Address == "" {
		return UnsubscribeResult{}, emailconsent.ErrUnsubscribeTokenInvalid
	}
	if _, err := s.unsubscribe(ctx, msg.WorkspaceID, msg.Address, source); err != nil {
		return UnsubscribeResult{}, err
	}
	if msg.UnsubscribedAt == nil {
		at := s.now()
		msg.UnsubscribedAt = &at
		if err := s.deps.Messages.Update(ctx, msg); err != nil {
			return UnsubscribeResult{}, err
		}
	}
	return UnsubscribeResult{WorkspaceID: msg.WorkspaceID, Address: msg.Address}, nil
}

func (s *Service) unsubscribe(ctx context.Context, workspaceID string, address emailaddress.Address, source emailconsent.UnsubscribeSource) (emailconsent.Subscription, error) {
	now := s.now()
	current, err := s.deps.Subscriptions.Get(ctx, workspaceID, address)
	if errors.Is(err, emailconsent.ErrSubscriptionNotFound) {
		leadID, err := s.leadFor(workspaceID, address)
		if err != nil {
			return emailconsent.Subscription{}, err
		}
		record, err := emailconsent.NewUnsubscribed(workspaceID, address.String(), leadID, source, now)
		if err != nil {
			return emailconsent.Subscription{}, err
		}
		return s.deps.Subscriptions.Save(ctx, record)
	}
	if err != nil {
		return emailconsent.Subscription{}, err
	}
	next, changed, err := current.Unsubscribe(source, now)
	if err != nil || !changed {
		return next, err
	}
	return s.deps.Subscriptions.Save(ctx, next)
}

var _ emailsending.Unsubscriber = (*Service)(nil)
