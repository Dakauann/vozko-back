package copilot_usecase

import (
	"context"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
)

func TestAReplyStoresItsTranscriptForTheNextOne(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	rt := &fakeTool{name: "list_agents", meta: readMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("list_agents", nil)}, {}}, texts: []string{"", "Você tem 3 agentes."}}
	if err := newService(prov, th, ms, rt).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "quantos agentes?"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
	turn, err := aichat.DecodeTranscript(ms.last().Transcript)
	if err != nil {
		t.Fatal(err)
	}
	if len(turn) != 3 || turn[0].ToolCalls[0].Name != "list_agents" || turn[1].Role != ai.RoleTool || turn[2].Content != "Você tem 3 agentes." {
		t.Fatalf("transcript = %+v, want the call, its result and the answer", turn)
	}
}

func storedTurn(t *testing.T, model string) *aichat.Message {
	t.Helper()
	raw, err := aichat.EncodeTranscript([]ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_agents"}}, Reasoning: []ai.ReasoningBlock{{Kind: ai.ReasoningText, Text: "plano", Signature: "sig"}}},
		{Role: ai.RoleTool, ToolCallID: "c1", Content: `{"total":3}`},
		{Role: ai.RoleAssistant, Content: "Você tem 3 agentes."},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &aichat.Message{Role: aichat.RoleAssistant, Content: "Você tem 3 agentes.", Model: model, Transcript: raw}
}

func TestTheNextReplyReplaysThePreviousTranscript(t *testing.T) {
	ms := &fakeMessages{list: []*aichat.Message{{Role: aichat.RoleUser, Content: "quantos agentes?"}, storedTurn(t, "m")}}
	h, err := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, ms).buildHistory("t1", "m", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 4 || h[1].ToolCalls[0].Name != "list_agents" || len(h[1].Reasoning) != 1 || h[2].Content != `{"total":3}` || h[3].Content != "Você tem 3 agentes." {
		t.Fatalf("history = %+v, want the request and the whole previous reply", h)
	}
}

func TestAnotherModelGetsThePreviousReplyWithoutItsReasoning(t *testing.T) {
	ms := &fakeMessages{list: []*aichat.Message{{Role: aichat.RoleUser, Content: "quantos agentes?"}, storedTurn(t, "outro")}}
	h, err := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, ms).buildHistory("t1", "m", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 4 || len(h[1].Reasoning) != 0 || h[1].ToolCalls[0].Name != "list_agents" {
		t.Fatalf("history = %+v, want the tool steps without reasoning", h)
	}
}

func TestAnUnreadableTranscriptFailsTheRebuildInsteadOfGuessing(t *testing.T) {
	broken := &aichat.Message{Role: aichat.RoleAssistant, Content: "x", Model: "m", Transcript: []byte("{broken")}
	ms := &fakeMessages{list: []*aichat.Message{broken}}
	if _, err := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, ms).buildHistory("t1", "m", true); err == nil {
		t.Fatal("an unreadable transcript must fail closed")
	}
}
