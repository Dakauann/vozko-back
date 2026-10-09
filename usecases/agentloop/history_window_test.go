package agentloop

import (
	"context"
	"fmt"
	"testing"

	"vozko/domain/ai"
)

func TestTrimmingKeepsTheSamePrefixUntilABlockFills(t *testing.T) {
	history := []ai.Message{{Role: ai.RoleUser, Content: "pedido"}}
	for i := 1; i < 81; i++ {
		history = append(history, ai.Message{Role: ai.RoleAssistant, Content: fmt.Sprintf("a%d", i)})
	}
	first := trimHistory(history, 80)
	for i := 81; i < 100; i++ {
		history = append(history, ai.Message{Role: ai.RoleAssistant, Content: fmt.Sprintf("a%d", i)})
		got := trimHistory(history, 80)
		if got[0].Content != "pedido" || got[1].Content != first[1].Content {
			t.Fatalf("after %d messages the kept prefix moved from %q to %q", len(history), first[1].Content, got[1].Content)
		}
	}
	if len(first) > 80 {
		t.Fatalf("the trimmed history must fit the cap, got %d", len(first))
	}
}

func TestTheLiveRequestIsFramedByUserRequest(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{fullText: "pronto"}}}
	e := Engine{AI: prov}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, Config{}, &Session{}, "faça X")
	messages := prov.inputs[0].Messages
	if messages[len(messages)-2].Content != UserRequest("faça X") {
		t.Fatalf("the request must be framed the same way history rebuilds it, got %q", messages[len(messages)-2].Content)
	}
}
