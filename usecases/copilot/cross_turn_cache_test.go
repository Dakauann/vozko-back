package copilot_usecase

import (
	"fmt"
	"strings"
	"testing"

	"vozko/domain/aichat"
	"vozko/domain/readiness"
	"vozko/usecases/agentloop"
)

type pagedMessages struct {
	fakeMessages
}

func (p *pagedMessages) ListByThread(in aichat.ListMessagesInput) ([]*aichat.Message, int64, error) {
	total := len(p.list)
	start := min(in.Offset, total)
	end := total
	if in.Limit > 0 {
		end = min(start+in.Limit, total)
	}
	return p.list[start:end], int64(total), nil
}

func (p *pagedMessages) add(n int) {
	for i := 0; i < n; i++ {
		index := len(p.list)
		role := aichat.RoleUser
		if index%2 == 1 {
			role = aichat.RoleAssistant
		}
		p.list = append(p.list, &aichat.Message{ThreadID: "th", Role: role, Content: fmt.Sprintf("m%d", index)})
	}
}

func TestHistoryRebuildsTheLastRequestExactlyAndMovesItsWindowInBlocks(t *testing.T) {
	messages := &pagedMessages{}
	messages.add(45)
	svc := NewService(agentloop.Engine{}, NewRegistry(), &fakeAccess{}, openFunds{}, &fakeThreads{thread: testThread()}, messages, nil, nil, func() string { return "a" })

	history, err := svc.buildHistory("th")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 35 || history[0].Content != agentloop.UserRequest("m10") {
		t.Fatalf("history starts at %q with %d messages", history[0].Content, len(history))
	}
	messages.add(4)
	later, err := svc.buildHistory("th")
	if err != nil {
		t.Fatal(err)
	}
	if later[0].Content != history[0].Content {
		t.Fatalf("two more turns must keep the window start, got %q", later[0].Content)
	}
}

func TestTheWorkspaceSnapshotRidesInTheTrailingNoteNotTheSystemPrompt(t *testing.T) {
	drv := NewDriver(ownerCtx, "m", NewRegistry(), &fakeAccess{}, openFunds{}, nil)
	drv.state = &readiness.Snapshot{SubscriptionActive: true, BalanceMicros: 2_500_000}
	if strings.Contains(drv.SystemPrompt(), "Estado do workspace") {
		t.Fatal("the balance changes every turn, so it must stay out of the cached system prompt")
	}
	if note := drv.Reground(1, 10, 0); !strings.Contains(note, "Estado do workspace") || !strings.Contains(note, "2.50") {
		t.Fatalf("the trailing note must carry the snapshot, got %q", note)
	}
}
