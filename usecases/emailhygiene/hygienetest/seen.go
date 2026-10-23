package hygienetest

import (
	"context"
	"sync"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
)

type SeenEmails struct {
	mu   sync.Mutex
	seen []emailaddress.Address
}

var _ h.AddressSeen = (*SeenEmails)(nil)

func (s *SeenEmails) AddressesSeen(_ context.Context, addresses []emailaddress.Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, addresses...)
}

func (s *SeenEmails) Seen() []emailaddress.Address {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]emailaddress.Address(nil), s.seen...)
}
