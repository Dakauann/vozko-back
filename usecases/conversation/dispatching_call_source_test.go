package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	conversation_domain "vozko/domain/conversation"
)

type namedSource struct {
	name   string
	dialed int
}

func (s *namedSource) Name() string { return s.name }
func (s *namedSource) Dial(context.Context, conversation_domain.CallDialInput) (conversation_domain.CRMCall, error) {
	s.dialed++
	return nil, nil
}

func TestDispatchingRoutesTrunkCallsToTheTrunkSourceAndTheRestToWhatsApp(t *testing.T) {
	whatsapp, trunks := &namedSource{name: "whatsapp"}, &namedSource{name: "sip"}
	source := NewDispatchingCallSource(whatsapp, trunks)

	_, _ = source.Dial(context.Background(), conversation_domain.CallDialInput{TrunkID: "trunk-1", PhoneNumber: "100"})
	_, _ = source.Dial(context.Background(), conversation_domain.CallDialInput{WhatsAppPhoneID: "phone-1", PhoneNumber: "100"})

	if trunks.dialed != 1 || whatsapp.dialed != 1 {
		t.Fatalf("trunk dials = %d, whatsapp dials = %d, want 1 each", trunks.dialed, whatsapp.dialed)
	}
}

func TestDispatchingRefusesATrunkCallWithoutATrunkSource(t *testing.T) {
	whatsapp := &namedSource{name: "whatsapp"}
	source := NewDispatchingCallSource(whatsapp, nil)
	_, err := source.Dial(context.Background(), conversation_domain.CallDialInput{TrunkID: "trunk-1"})
	if !errors.Is(err, conversation_domain.ErrNoCallSource) || whatsapp.dialed != 0 {
		t.Fatalf("Dial() = %v (whatsapp dials %d), want ErrNoCallSource and no fallback to WhatsApp", err, whatsapp.dialed)
	}
}
