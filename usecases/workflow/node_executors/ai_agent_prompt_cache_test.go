package node_executors

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/rag"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type recordingAgentAI struct {
	ai.Service
	inputs []ai.GenerateInput
}

func (r *recordingAgentAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	r.inputs = append(r.inputs, in)
	return &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: "Certo, anotado."}}, nil
}

type entryTranscript struct {
	conversation.MessageRepository
	history []*conversation.Message
}

func (e *entryTranscript) ListByEntry(string, shared.EntryType) ([]*conversation.Message, error) {
	return e.history, nil
}

type knowledgeBase struct{ rag.RAGService }

func (knowledgeBase) Query(context.Context, rag.QueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: []rag.QueryResult{{Content: "Entregamos em até 40 minutos.", DocumentName: "faq.pdf", Score: 0.9}}}, nil
}

func agentNodeTranscript(n int) []*conversation.Message {
	history := make([]*conversation.Message, 0, n)
	for i := range n {
		if i%2 == 0 {
			history = append(history, &conversation.Message{Text: fmt.Sprintf("pergunta %d", i), MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511")})
			continue
		}
		history = append(history, &conversation.Message{Text: fmt.Sprintf("resposta %d", i), MessageType: conversation.MessageTypeAIResponse, SentBy: conversation.SentByAI("agent-1")})
	}
	return history
}

func agentNodeContext(state *workflow.RunState, config map[string]interface{}) *workflow.NodeContext {
	base := map[string]interface{}{
		"source":         "prompt",
		"model":          "openai/gpt-4o-mini",
		"instructions":   "Você atende a loja.",
		"context_window": float64(50),
	}
	for key, value := range config {
		base[key] = value
	}
	return &workflow.NodeContext{
		Run:      &workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: string(shared.EntryTypeTelegram)},
		Workflow: &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1"},
		Node:     &workflow.Node{ID: "ai-1", Type: workflow.NodeTypeActionAIAgent, Config: base},
		Graph:    &workflow.Graph{},
		State:    state,
	}
}

func lastAgentMessage(input ai.GenerateInput) string {
	return input.Messages[len(input.Messages)-1].Content
}

func TestTheAgentNodeKeepsItsPrefixAcrossCalls(t *testing.T) {
	recorder := &recordingAgentAI{}
	transcript := &entryTranscript{history: agentNodeTranscript(60)}
	executor := NewAIAgentExecutor(recorder, nil, transcript, nil, nil, nil)
	state := workflow.NewRunState()
	config := map[string]interface{}{"additional_context": "Pedido do cliente: {{pedido}}"}

	state.Set("pedido", "pizza")
	if _, err := executor.Execute(agentNodeContext(&state, config)); err != nil {
		t.Fatal(err)
	}
	transcript.history = agentNodeTranscript(61)
	state.Set("pedido", "pizza grande")
	if _, err := executor.Execute(agentNodeContext(&state, config)); err != nil {
		t.Fatal(err)
	}

	if len(recorder.inputs) != 2 {
		t.Fatalf("calls = %d", len(recorder.inputs))
	}
	before, after := recorder.inputs[0], recorder.inputs[1]
	if before.SystemPrompt != "Você atende a loja." || after.SystemPrompt != before.SystemPrompt {
		t.Fatalf("the system prompt must hold only the instructions:\nbefore: %q\nafter: %q", before.SystemPrompt, after.SystemPrompt)
	}
	if after.VolatileTail != 1 || after.SessionID != "workflow_agent:entry-1" || after.BillingReference != "workflow_agent:entry-1" {
		t.Fatalf("tail %d, session %q, billing %q", after.VolatileTail, after.SessionID, after.BillingReference)
	}
	for i := range before.Messages[:len(before.Messages)-1] {
		if before.Messages[i].Role != after.Messages[i].Role || before.Messages[i].Content != after.Messages[i].Content {
			t.Fatalf("history message %d changed between calls: %q then %q", i, before.Messages[i].Content, after.Messages[i].Content)
		}
	}
	note := lastAgentMessage(after)
	if !strings.HasPrefix(note, ai.ContextNote().Content) {
		t.Fatalf("the last message must be the system context note:\n%s", note)
	}
	for _, want := range []string{"Pedido do cliente: pizza grande", "AÇÕES JÁ EXECUTADAS NESTA CONVERSA", "Resposta enviada: Certo, anotado."} {
		if !strings.Contains(note, want) {
			t.Errorf("the note lacks %q:\n%s", want, note)
		}
	}
	if strings.Contains(lastAgentMessage(before), "AÇÕES JÁ EXECUTADAS") {
		t.Error("the first call had no executed actions yet")
	}
}

func TestTheAgentNodeHistoryWindowOnlyMovesInBlocks(t *testing.T) {
	recorder := &recordingAgentAI{}
	transcript := &entryTranscript{}
	executor := NewAIAgentExecutor(recorder, nil, transcript, nil, nil, nil)
	for _, total := range []int{60, 61, 62} {
		transcript.history = agentNodeTranscript(total)
		state := workflow.NewRunState()
		if _, err := executor.Execute(agentNodeContext(&state, nil)); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range recorder.inputs {
		if input.Messages[0].Content != "pergunta 12" {
			t.Fatalf("the window must start at the block boundary, got %q", input.Messages[0].Content)
		}
	}
}

func TestTheAgentNodeSendsKnowledgeInTheNote(t *testing.T) {
	recorder := &recordingAgentAI{}
	executor := NewAIAgentExecutor(recorder, nil, &entryTranscript{history: agentNodeTranscript(5)}, nil, nil, knowledgeBase{})
	state := workflow.NewRunState()
	if _, err := executor.Execute(agentNodeContext(&state, map[string]interface{}{"knowledge_base_ids": []interface{}{"kb-1"}})); err != nil {
		t.Fatal(err)
	}
	input := recorder.inputs[0]
	if strings.Contains(input.SystemPrompt, "Entregamos em até 40 minutos.") {
		t.Fatal("retrieved knowledge changes per question, so it never belongs to the system prompt")
	}
	if !strings.Contains(lastAgentMessage(input), "Entregamos em até 40 minutos.") {
		t.Fatalf("the note lacks the retrieved knowledge:\n%s", lastAgentMessage(input))
	}
	if input.Messages[len(input.Messages)-2].Content != "pergunta 4" {
		t.Fatalf("the note must follow the conversation, got %q before it", input.Messages[len(input.Messages)-2].Content)
	}
}

func TestTheAgentNodeAlwaysEndsWithTheNote(t *testing.T) {
	recorder := &recordingAgentAI{}
	executor := NewAIAgentExecutor(recorder, nil, &entryTranscript{history: agentNodeTranscript(3)}, nil, nil, nil)
	state := workflow.NewRunState()
	if _, err := executor.Execute(agentNodeContext(&state, nil)); err != nil {
		t.Fatal(err)
	}
	input := recorder.inputs[0]
	if input.VolatileTail != 1 || lastAgentMessage(input) != ai.ContextNote().Content {
		t.Fatalf("with nothing volatile the note still carries the clock: tail %d, last %q", input.VolatileTail, lastAgentMessage(input))
	}
}
