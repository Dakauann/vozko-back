package copilot_usecase

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/usecases/agentloop"
)

func TestAProviderFailureTellsTheUserWithoutTheTechnicalCause(t *testing.T) {
	halt := fmt.Errorf("%w: %w", agentloop.ErrProviderFailed, errors.New("stream: error, code: 402, message: Insufficient credits"))
	msg := haltMessage(halt)
	if msg != msgProviderFailed {
		t.Fatalf("haltMessage = %q, want the provider failure message", msg)
	}
	if strings.Contains(msg, "402") || strings.Contains(strings.ToLower(msg), "credits") {
		t.Fatal("the user must never see the provider's internal reason")
	}
}
