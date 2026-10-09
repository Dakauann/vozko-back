package agentloop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAProviderFailureHaltsTheRunWithAReasonTheCallerCanShow(t *testing.T) {
	e := Engine{AI: &fakeAI{errAt: 1}}
	out := e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, Config{}, &Session{}, "oi")
	if !errors.Is(out.Halt, ErrProviderFailed) || out.Valid {
		t.Fatalf("a refused call must halt as a provider failure, got %+v", out)
	}
	if !strings.Contains(out.Halt.Error(), "provider boom") {
		t.Fatalf("the provider's own reason must stay in the error for the logs, got %v", out.Halt)
	}
}
