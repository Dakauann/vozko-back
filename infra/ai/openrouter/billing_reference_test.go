package openrouter

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
)

func TestAStreamedChargeCarriesItsBillingReference(t *testing.T) {
	srv := sseServer([]string{
		`{"choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"cost":0.001}}`,
	})
	defer srv.Close()
	pub := &capturingPub{}
	ch, err := newTestService(srv.URL, pub).GenerateStream(context.Background(), ai.GenerateInput{
		Model:            "anthropic/claude",
		WorkspaceID:      "ws-1",
		BillingReference: "aichat:th-1",
		Messages:         []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ch)
	events := pub.events()
	if len(events) != 1 || !strings.HasPrefix(events[0].RequestID, "aichat:th-1:") {
		t.Fatalf("the charge must name its thread, got %+v", events)
	}
}

func TestARecoveredChargeKeepsItsBillingReference(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-ref-1","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
	})
	defer srv.Close()
	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	svc.usageFetcher = &stubFetcher{pt: 10, ct: 5, ok: true}
	ch, err := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:            "anthropic/claude",
		WorkspaceID:      "ws-1",
		BillingReference: "aichat:th-2",
		Messages:         []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(pub.events()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	events := pub.events()
	if len(events) != 1 || !strings.HasPrefix(events[0].RequestID, "aichat:th-2:") {
		t.Fatalf("a recovered charge must still name its thread, got %+v", events)
	}
}
