package carriermessaging

import (
	"errors"
	"testing"
)

func TestRefusalErrorsMatchTheirSentinels(t *testing.T) {
	if !errors.Is(&QuietHoursError{NextOpen: t0}, ErrQuietHours) {
		t.Fatal("quiet hours error must match ErrQuietHours")
	}
	var short *ShortfallError
	err := error(&ShortfallError{MissingMicros: 10})
	if !errors.Is(err, ErrInsufficientBalance) || !errors.As(err, &short) || short.MissingMicros != 10 {
		t.Fatalf("shortfall = %v", err)
	}
}
