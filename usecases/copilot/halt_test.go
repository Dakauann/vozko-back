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

func TestAFailedCompactionTellsTheUserWhatHappened(t *testing.T) {
	if haltMessage(agentloop.ErrCompactionFailed) != msgCompactionFailed {
		t.Fatal("a reply stopped because the context could not be summarized must say so")
	}
}

func TestARepeatedActionStopTellsTheUserWhy(t *testing.T) {
	if haltMessage(agentloop.ErrRepeatedTurn) != msgRepeatedTurn {
		t.Fatal("a stop for repeating the same action must explain itself to the user")
	}
}
