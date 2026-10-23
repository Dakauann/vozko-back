package emailhygiene

import (
	"context"
	"errors"
	"log"
	"time"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
)

var ErrAddressInvalid = errors.New("emailhygiene: the address is not valid")

type ServiceDeps struct {
	Standing   h.StandingReader
	Domains    *Domains
	Disposable *Disposable
}

type Service struct {
	d   ServiceDeps
	now func() time.Time
}

func NewService(d ServiceDeps) *Service {
	return &Service{d: d, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Facts(a emailaddress.Address) h.Facts {
	return h.NewChecker(s.d.Disposable.Set(), h.DefaultTypos()).Facts(a)
}

func (s *Service) Standing(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]h.Standing, error) {
	return s.d.Standing.Standing(ctx, workspaceID, addresses)
}

func (s *Service) RequestDomains(ctx context.Context, domains []string) error {
	return s.d.Domains.Request(ctx, domains)
}

func (s *Service) AddressesSeen(ctx context.Context, addresses []emailaddress.Address) {
	domains := make([]string, 0, len(addresses))
	for _, a := range addresses {
		domains = append(domains, a.Domain())
	}
	if err := s.RequestDomains(ctx, domains); err != nil {
		log.Printf("[emailhygiene] the domains of %d seen addresses were not queued, the first send queues them: %v", len(addresses), err)
	}
}

func (s *Service) ResolveDomains(ctx context.Context, domains []string) (map[string]h.DomainState, error) {
	return s.d.Domains.Resolve(ctx, domains)
}

func (s *Service) RequestStale(ctx context.Context, standing map[emailaddress.Address]h.Standing) error {
	var stale []string
	for address, st := range standing {
		if st.DomainStale {
			stale = append(stale, address.Domain())
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return s.RequestDomains(ctx, stale)
}

type AddressStatus struct {
	Address   emailaddress.Address
	Verdict   h.Verdict
	Facts     h.Facts
	Standing  h.Standing
	Suggested emailaddress.Address
	Inactive  bool
}

func (s *Service) AddressStatus(ctx context.Context, workspaceID, raw string) (AddressStatus, error) {
	address, err := emailaddress.Normalize(raw)
	if err != nil {
		return AddressStatus{}, ErrAddressInvalid
	}
	standing, err := s.Standing(ctx, workspaceID, []emailaddress.Address{address})
	if err != nil {
		return AddressStatus{}, err
	}
	if err := s.RequestStale(ctx, standing); err != nil {
		return AddressStatus{}, err
	}
	st := standing[address]
	now := s.now()
	facts, a := s.assess(address, st, now)
	return AddressStatus{Address: address, Verdict: a.Verdict, Facts: facts, Standing: st, Suggested: a.Suggested, Inactive: st.Activity.Inactive(now)}, nil
}

func (s *Service) AssessMany(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]h.Assessment, error) {
	standing, err := s.Standing(ctx, workspaceID, addresses)
	if err != nil {
		return nil, err
	}
	var stale []string
	for address, st := range standing {
		if st.DomainStale {
			stale = append(stale, address.Domain())
		}
	}
	if len(stale) > 0 {
		resolved, err := s.ResolveDomains(ctx, stale)
		if err != nil {
			return nil, err
		}
		for address, st := range standing {
			if state, ok := resolved[address.Domain()]; ok && st.DomainStale {
				st.Domain, st.DomainStale = state, false
				standing[address] = st
			}
		}
	}
	now := s.now()
	out := make(map[emailaddress.Address]h.Assessment, len(standing))
	for address, st := range standing {
		_, out[address] = s.assess(address, st, now)
	}
	return out, nil
}

func (s *Service) assess(address emailaddress.Address, st h.Standing, now time.Time) (h.Facts, h.Assessment) {
	facts := s.Facts(address)
	a := h.Assessment{Verdict: h.Assess(facts, st, now)}
	if fixed, ok := h.SuggestedAddress(address, facts.Suggestion); ok {
		a.Suggested = fixed
	}
	return facts, a
}

func (s *Service) SendRefusal(ctx context.Context, workspaceID string, kind es.RunKind, to emailaddress.Address) error {
	if !h.FollowsCampaignRule(kind) {
		return nil
	}
	standing, err := s.Standing(ctx, workspaceID, []emailaddress.Address{to})
	if err != nil {
		return err
	}
	st := standing[to]
	if st.NeedsDomainCheck() {
		if err := s.RequestDomains(ctx, []string{to.Domain()}); err != nil {
			return err
		}
	}
	if refusal := h.OneToOneRefusal(kind, s.Facts(to), st, s.now()); refusal != nil {
		return refusal
	}
	return nil
}
