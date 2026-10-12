package agentloop

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/ai"
)

type inboxDriver struct {
	*fakeDriver
	arrivals map[int][]string
	drains   int
	seen     [][]ai.Message
	refuseAt int
	admits   int
}

func (d *inboxDriver) Admit(context.Context) error {
	d.admits++
	if d.admits == d.refuseAt {
		return errBroke
	}
	return nil
}

func (d *inboxDriver) Drain(_ context.Context, history []ai.Message) []ai.Message {
	d.drains++
	d.seen = append(d.seen, append([]ai.Message(nil), history...))
	var out []ai.Message
	for _, text := range d.arrivals[d.drains] {
		out = append(out, ai.Message{Role: ai.RoleUser, Content: Interjection(text)})
	}
	return out
}

func lastUserTexts(in ai.GenerateInput) []string {
	var out []string
	for _, m := range in.Messages {
		if m.Role == ai.RoleUser {
			out = append(out, m.Content)
		}
	}
	return out
}

func TestRun_AMessageThatArrivesDuringAToolIsSeenByTheNextCallAfterTheResults(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}}, {tokens: []string{"feito"}}}}
	drv := &inboxDriver{fakeDriver: &fakeDriver{}, arrivals: map[int][]string{2: {"use azul"}}}
	out := runInbox(prov, drv, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeIdle {
		t.Fatalf("outcome = %+v", out)
	}
	if len(prov.inputs) != 2 {
		t.Fatalf("model calls = %d, a delivered message must not add or repeat a call", len(prov.inputs))
	}
	second := prov.inputs[1].Messages
	n := len(second)
	if second[n-1].Role != ai.RoleUser || second[n-2].Content != Interjection("use azul") || second[n-3].Role != ai.RoleTool {
		t.Fatalf("want tool result, then the new message, then the reground note; got %+v", second[n-3:])
	}
	for _, text := range lastUserTexts(prov.inputs[0]) {
		if text == Interjection("use azul") {
			t.Fatal("the first call already ran; it must not see a message that arrived later")
		}
	}
}

func TestRun_DeliveryAppendsAtTheEndSoTheCachedPrefixStays(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}}, {tokens: []string{"feito"}}}}
	drv := &inboxDriver{fakeDriver: &fakeDriver{}, arrivals: map[int][]string{2: {"mais uma coisa"}}}
	runInbox(prov, drv, Config{FinishToolName: "finish"})
	first, second := prov.inputs[0], prov.inputs[1]
	if first.SystemPrompt != second.SystemPrompt || !reflect.DeepEqual(first.Tools, second.Tools) {
		t.Fatal("system prompt and tools must not change when a message is delivered")
	}
	prefix := first.Messages[:len(first.Messages)-1]
	if !reflect.DeepEqual(second.Messages[:len(prefix)], prefix) {
		t.Fatal("the earlier conversation must stay a byte-identical prefix")
	}
}

func TestRun_TheInboxIsDrainedOnlyAfterTheGuardAdmitsTheCall(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}}, {tokens: []string{"feito"}}}}
	drv := &inboxDriver{fakeDriver: &fakeDriver{}, refuseAt: 2, arrivals: map[int][]string{2: {"x"}}}
	out, _ := runGuarded(prov, drv)
	if out.Halt == nil || len(prov.inputs) != 1 {
		t.Fatalf("outcome %+v calls %d", out, len(prov.inputs))
	}
	if drv.drains != 1 {
		t.Fatalf("drains = %d, a refused call must leave the queue for the next turn", drv.drains)
	}
}

func TestRun_TheInboxIsAskedOncePerModelCall(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}}, {tcs: []ai.ToolCall{tcall("read")}}, {tokens: []string{"ok"}}}}
	drv := &inboxDriver{fakeDriver: &fakeDriver{}}
	runInbox(prov, drv, Config{FinishToolName: "finish"})
	if drv.drains != len(prov.inputs) || drv.drains != 3 {
		t.Fatalf("drains = %d calls = %d", drv.drains, len(prov.inputs))
	}
	last := drv.seen[2]
	if last[len(last)-1].Role != ai.RoleTool {
		t.Fatalf("the inbox must see the history after the tool results, got %+v", last[len(last)-1])
	}
}

func TestRun_ASpentBudgetAnswersWithoutDraining(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("read")}, usage: &ai.Usage{TotalTokens: 100}}, {tokens: []string{"resumo"}}}}
	drv := &inboxDriver{fakeDriver: &fakeDriver{}, arrivals: map[int][]string{2: {"tarde demais"}}}
	runInbox(prov, drv, Config{FinishToolName: "finish", SessionTokenBudget: 50, GraceInstruction: "resuma"})
	if drv.drains != 1 {
		t.Fatalf("drains = %d, the grace answer must not swallow queued messages", drv.drains)
	}
}

func runInbox(prov *fakeAI, drv Driver, cfg Config) Outcome {
	e := Engine{AI: prov}
	return e.Run(context.Background(), (&capture{}).emit, drv, cfg, &Session{}, "faça X")
}
