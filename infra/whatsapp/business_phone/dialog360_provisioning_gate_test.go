package whatsapp_business_phone

import (
	"errors"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type stubGate struct {
	ok  bool
	err error
}

func (g stubGate) CanProvisionPhone(string) (bool, error) { return g.ok, g.err }

func TestCheckProvisioningAllowed_DeniesWhenOverLimit(t *testing.T) {
	s := (&Dialog360OnboardingService{}).WithProvisioningGate(stubGate{ok: false})
	if err := s.checkProvisioningAllowed("ws"); !errors.Is(err, businessphone.ErrPhoneLimitReached) {
		t.Fatalf("expected ErrPhoneLimitReached, got %v", err)
	}
}

func TestCheckProvisioningAllowed_AllowsWhenUnderLimit(t *testing.T) {
	s := (&Dialog360OnboardingService{}).WithProvisioningGate(stubGate{ok: true})
	if err := s.checkProvisioningAllowed("ws"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckProvisioningAllowed_FailsClosedOnGateError(t *testing.T) {
	boom := errors.New("db unavailable")
	s := (&Dialog360OnboardingService{}).WithProvisioningGate(stubGate{ok: true, err: boom})
	err := s.checkProvisioningAllowed("ws")
	if err == nil {
		t.Fatal("a gate error must deny provisioning (fail-closed), got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected the underlying gate error to propagate, got %v", err)
	}
}

func TestCheckProvisioningAllowed_NoGateAllows(t *testing.T) {
	s := &Dialog360OnboardingService{}
	if err := s.checkProvisioningAllowed("ws"); err != nil {
		t.Fatalf("expected nil when no gate configured, got %v", err)
	}
}
