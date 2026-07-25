package conversation_usecase

import (
	"errors"
	"testing"
	"time"

	dialer_domain "vozko/domain/dialer"
)

type stubOwnership struct {
	owned map[string]bool
}

func (s *stubOwnership) IsOwner(trunkID string) bool { return s.owned[trunkID] }

func (s *stubOwnership) FindOwnerAddress(trunkID string) (string, string, error) {
	return "", "", nil
}

func TestMaybeTrunkBusy_NilOwnership_ReturnsNil(t *testing.T) {
	src := &SIPTrunkCallSource{}
	if got := src.maybeTrunkBusy("trunk-1"); got != nil {
		t.Fatalf("maybeTrunkBusy = %v, want nil", got)
	}
}

func TestMaybeTrunkBusy_LocalReplicaIsOwner_ReturnsNil(t *testing.T) {
	src := &SIPTrunkCallSource{
		ownership: &stubOwnership{owned: map[string]bool{"trunk-1": true}},
	}
	if got := src.maybeTrunkBusy("trunk-1"); got != nil {
		t.Fatalf("maybeTrunkBusy = %v, want nil for owned trunk", got)
	}
}

func TestMaybeTrunkBusy_NotOwner_ReturnsTrunkBusy(t *testing.T) {
	src := &SIPTrunkCallSource{
		ownership: &stubOwnership{owned: map[string]bool{}},
	}
	got := src.maybeTrunkBusy("trunk-1")
	if got == nil {
		t.Fatal("maybeTrunkBusy = nil, want *ErrTrunkBusy")
	}
	if got.TrunkID != "trunk-1" {
		t.Errorf("TrunkID = %q, want trunk-1", got.TrunkID)
	}
	if got.Reason != dialer_domain.TrunkBusyReasonReconciling {
		t.Errorf("Reason = %q, want %q", got.Reason, dialer_domain.TrunkBusyReasonReconciling)
	}
	if got.RetryAfter <= 0 || got.RetryAfter > 10*time.Second {
		t.Errorf("RetryAfter = %v, want a positive bounded duration", got.RetryAfter)
	}
}

func TestMaybeTrunkBusy_TypedErrorIsUnwrappable(t *testing.T) {
	src := &SIPTrunkCallSource{
		ownership: &stubOwnership{owned: map[string]bool{}},
	}
	busy := src.maybeTrunkBusy("trunk-1")
	if busy == nil {
		t.Fatal("setup: maybeTrunkBusy returned nil")
	}
	var asErr error = busy
	got := dialer_domain.AsTrunkBusy(asErr)
	if got == nil {
		t.Fatal("AsTrunkBusy returned nil for typed error")
	}

	wrapped := wrapErr("dial failed: %w", asErr)
	if dialer_domain.AsTrunkBusy(wrapped) == nil {
		t.Fatal("AsTrunkBusy returned nil for wrapped error")
	}
}

func wrapErr(_ string, err error) error {
	return wrappingErr{inner: err}
}

type wrappingErr struct{ inner error }

func (w wrappingErr) Error() string { return "wrap: " + w.inner.Error() }
func (w wrappingErr) Unwrap() error { return w.inner }

var _ = errors.As
