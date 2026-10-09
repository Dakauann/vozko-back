package agentloop

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
)

func imageMessages(msgs []ai.Message) []ai.Message {
	var out []ai.Message
	for _, m := range msgs {
		if m.Role == ai.RoleUser && strings.HasPrefix(m.Content, toolImagesNote) {
			out = append(out, m)
		}
	}
	return out
}

func TestRun_ToolImagesReachTheModelRightAfterTheirResults(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("look")}},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}
	drv := &fakeDriver{dispatchFn: func(ai.ToolCall) StepResult {
		return StepResult{Result: "quadros capturados", Images: []string{"data:image/jpeg;base64,AAAA"}}
	}}
	run(t, prov, drv, Config{FinishToolName: "finish"})
	second := prov.inputs[1].Messages
	var toolAt, imagesAt = -1, -1
	for i, m := range second {
		if m.Role == ai.RoleTool {
			toolAt = i
		}
		if len(m.Images) > 0 && strings.HasPrefix(m.Content, toolImagesNote) {
			imagesAt = i
		}
	}
	if toolAt < 0 || imagesAt != toolAt+1 {
		t.Fatalf("images must follow the tool result: tool at %d, images at %d", toolAt, imagesAt)
	}
}

func TestRun_OnlyTheMostRecentToolImagesStayInContext(t *testing.T) {
	look := aiTurn{tcs: []ai.ToolCall{tcall("look")}}
	prov := &fakeAI{turns: []aiTurn{look, look, look, {tcs: []ai.ToolCall{tcall("finish")}}}}
	drv := &fakeDriver{dispatchFn: func(call ai.ToolCall) StepResult {
		return StepResult{Result: "ok", Images: []string{"data:image/jpeg;base64,AAAA"}, Signature: call.ID}
	}}
	run(t, prov, drv, Config{FinishToolName: "finish", RepeatedTurnStop: 10})
	messages := imageMessages(prov.inputs[3].Messages)
	if len(messages) != 3 {
		t.Fatalf("expected three image notes, got %d", len(messages))
	}
	if len(messages[0].Images) != 0 || messages[0].Content != toolImagesCleared {
		t.Fatalf("the oldest images must be cleared, got %+v", messages[0])
	}
	if len(messages[1].Images) != 1 || len(messages[2].Images) != 1 {
		t.Fatal("the two most recent captures must stay")
	}
}

func TestRun_AToolCanEndTheTurnToWaitForTheUser(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tokens: []string{"Antes, uma pergunta."}, tcs: []ai.ToolCall{tcall("ask")}},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}
	drv := &fakeDriver{dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Result: "pergunta exibida", EndTurn: true} }}
	out, _, sess := run(t, prov, drv, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeIdle {
		t.Fatalf("expected the turn to end idle, got %+v", out)
	}
	if len(prov.inputs) != 1 {
		t.Fatalf("the model must not be called again after the question, got %d calls", len(prov.inputs))
	}
	last := sess.History[len(sess.History)-1]
	if last.Role != ai.RoleTool || last.Content != "pergunta exibida" {
		t.Fatalf("the question's tool result must be recorded, got %+v", last)
	}
}

func TestRun_ACallCutByTheOutputLimitIsNeverExecuted(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{{Name: "edit", Arguments: map[string]interface{}{"operations": "[{\"op\":"}}}, finish: "length"},
		{tcs: []ai.ToolCall{tcall("edit")}},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}
	drv := &fakeDriver{}
	run(t, prov, drv, Config{FinishToolName: "finish"})
	if len(drv.dispatched) != 1 {
		t.Fatalf("only the complete call may run, dispatched %v", drv.dispatched)
	}
	var told bool
	for _, m := range prov.inputs[1].Messages {
		if m.Role == ai.RoleTool && strings.HasPrefix(m.Content, truncatedCallResult) {
			told = true
		}
	}
	if !told {
		t.Fatal("the model must be told its call was cut and nothing ran")
	}
}

func TestRun_RepeatedCutCallsEndTheTurn(t *testing.T) {
	cut := aiTurn{tcs: []ai.ToolCall{tcall("edit")}, finish: "length"}
	prov := &fakeAI{turns: []aiTurn{cut, cut, cut, cut}}
	drv := &fakeDriver{}
	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", EmptyTurnRetries: 2})
	if len(drv.dispatched) != 0 || out.Summary != reasonEmptyTurn {
		t.Fatalf("dispatched %v, outcome %+v", drv.dispatched, out)
	}
}

func TestRun_AReplyCutBeforeAnyActionRetriesWithASmallerStepNudge(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{reasoning: []string{"planejando tudo"}, finish: "length"},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}
	run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish"})
	nudged := false
	for _, m := range prov.inputs[1].Messages {
		nudged = nudged || (m.Role == ai.RoleUser && m.Content == truncatedReplyNudge)
	}
	if !nudged {
		t.Fatal("the retry must tell the model to act in smaller steps")
	}
	if len(prov.inputs[0].Messages) == len(prov.inputs[1].Messages) {
		t.Fatal("the retry must not resend the same conversation")
	}
}

func TestRun_GivingUpOnCutRepliesHaltsWithAReason(t *testing.T) {
	cut := aiTurn{finish: "length"}
	prov := &fakeAI{turns: []aiTurn{cut, cut, cut, cut}}
	out, _, _ := run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish", EmptyTurnRetries: 2})
	if !errors.Is(out.Halt, ErrOutputTruncated) {
		t.Fatalf("outcome %+v must halt as truncated", out)
	}
	callCut := aiTurn{tcs: []ai.ToolCall{tcall("edit")}, finish: "length"}
	prov = &fakeAI{turns: []aiTurn{callCut, callCut, callCut, callCut}}
	out, _, _ = run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish", EmptyTurnRetries: 2})
	if !errors.Is(out.Halt, ErrOutputTruncated) {
		t.Fatalf("outcome %+v must halt as truncated", out)
	}
}
