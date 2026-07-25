package dialer

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestErrTrunkBusy_ErrorContainsAllFields(t *testing.T) {
	err := &ErrTrunkBusy{
		TrunkID:    "trunk-7",
		Reason:     TrunkBusyReasonReconciling,
		RetryAfter: 2500 * time.Millisecond,
	}
	msg := err.Error()
	for _, want := range []string{"trunk-7", TrunkBusyReasonReconciling, "2.5s"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, must contain %q", msg, want)
		}
	}
}

func TestErrTrunkBusy_DefaultsReasonToNotOwned(t *testing.T) {
	err := &ErrTrunkBusy{TrunkID: "t", RetryAfter: time.Second}
	if !strings.Contains(err.Error(), TrunkBusyReasonNotOwned) {
		t.Fatalf("expected default reason %q in %q", TrunkBusyReasonNotOwned, err.Error())
	}
}

func TestErrTrunkBusy_NilReceiver(t *testing.T) {
	var err *ErrTrunkBusy
	if got := err.Error(); got == "" {
		t.Fatalf("nil receiver Error() must not panic and must return a stable string, got %q", got)
	}
}

func TestAsTrunkBusy_NilReturnsNil(t *testing.T) {
	if got := AsTrunkBusy(nil); got != nil {
		t.Fatalf("AsTrunkBusy(nil) = %v, want nil", got)
	}
}

func TestAsTrunkBusy_PlainErrorReturnsNil(t *testing.T) {
	if got := AsTrunkBusy(errors.New("boom")); got != nil {
		t.Fatalf("AsTrunkBusy(plain) = %v, want nil", got)
	}
}

func TestAsTrunkBusy_ReturnsTypedErrorEvenWhenWrapped(t *testing.T) {
	inner := &ErrTrunkBusy{
		TrunkID:    "trunk-1",
		Reason:     TrunkBusyReasonRegistering,
		RetryAfter: 4 * time.Second,
	}
	wrapped := errFmt("wrap: %w", inner)

	got := AsTrunkBusy(wrapped)
	if got == nil {
		t.Fatal("AsTrunkBusy returned nil for typed error")
	}
	if got.TrunkID != inner.TrunkID || got.Reason != inner.Reason || got.RetryAfter != inner.RetryAfter {
		t.Errorf("AsTrunkBusy mismatched: %+v vs %+v", got, inner)
	}
}

func errFmt(format string, args ...interface{}) error {
	return wrappedErr{format: format, args: args}
}

type wrappedErr struct {
	format string
	args   []interface{}
}

func (w wrappedErr) Error() string {
	for _, a := range w.args {
		if e, ok := a.(error); ok {
			return "wrap: " + e.Error()
		}
	}
	return w.format
}

func (w wrappedErr) Unwrap() error {
	for _, a := range w.args {
		if e, ok := a.(error); ok {
			return e
		}
	}
	return nil
}
