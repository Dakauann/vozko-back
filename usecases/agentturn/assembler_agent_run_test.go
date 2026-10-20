package agentturn

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/tools"
)

type agentRunLineStub struct {
	line  string
	asked []string
}

func (s *agentRunLineStub) ContextLine(workspaceID, entryID, entryType string) string {
	s.asked = append(s.asked, workspaceID+"/"+entryID+"/"+entryType)
	return s.line
}

func workflowToolAssembler(runs AgentRunContext) *Assembler {
	reg := stubRegistry{defs: []tools.Definition{
		{Name: "run_workflow", BindingKey: "workflow_id"},
		{Name: "book_meeting"},
	}}
	a := New(reg, echoRAG{}, nil)
	a.TrackAgentRuns(runs)
	return a
}

func workflowAgentTurn(history []ai.Message, customerMessage string) Request {
	req := turnRequest(leadIdentity("5511", "Rosalinda", "conv-ana", nil), history, customerMessage)
	req.Agent.InternalTools = []agent.ToolBinding{{Name: "run_workflow", Config: map[string]interface{}{"workflow_id": "wf-1"}}, {Name: "book_meeting"}}
	req.ToolSeed = map[string]interface{}{"__entry_id": "entry-1", "__entry_type": "instagram"}
	return req
}

func TestTheRunTheAgentStartedRidesInTheTrailingNoteNotTheCachedPrompt(t *testing.T) {
	runs := &agentRunLineStub{line: `Fluxo "Matrícula" em execução há 2 min, etapa: Requisição HTTP.`}
	a := workflowToolAssembler(runs)

	first := a.Assemble(context.Background(), workflowAgentTurn(nil, "e aí?"))
	runs.line = `Fluxo "Matrícula" concluído há 1 min. Resultado do fluxo: {"status":"ok"}`
	second := a.Assemble(context.Background(), workflowAgentTurn([]ai.Message{{Role: ai.RoleUser, Content: "e aí?"}}, "deu certo?"))

	if first.Input.SystemPrompt != second.Input.SystemPrompt || strings.Contains(first.Input.SystemPrompt, "Matrícula") {
		t.Fatalf("the run status must stay out of the cached system prompt:\n%s", first.Input.SystemPrompt)
	}
	note := second.Input.Messages[len(second.Input.Messages)-1]
	if note.Role != ai.RoleUser || !strings.Contains(note.Content, `Resultado do fluxo: {"status":"ok"}`) || second.Input.VolatileTail != 1 {
		t.Fatalf("note = %+v, tail = %d", note, second.Input.VolatileTail)
	}
	if strings.Index(note.Content, "TRECHO sobre") > strings.Index(note.Content, "Resultado do fluxo") {
		t.Fatalf("the run line must close the note:\n%s", note.Content)
	}
	if runs.asked[0] != "ws-1/entry-1/instagram" {
		t.Fatalf("asked = %v", runs.asked)
	}
}

func TestAnAgentWithoutTheWorkflowToolNeverAsksForRuns(t *testing.T) {
	runs := &agentRunLineStub{line: "x"}
	a := cachedAssembler(nil)
	a.TrackAgentRuns(runs)

	out := a.Assemble(context.Background(), turnRequest(leadIdentity("5511", "Rosalinda", "conv-ana", nil), nil, "oi"))

	if len(runs.asked) != 0 || strings.Contains(out.Input.Messages[len(out.Input.Messages)-1].Content, "FLUXO") {
		t.Fatalf("asked = %v", runs.asked)
	}
}

func TestNoRunMeansNoRunSection(t *testing.T) {
	a := workflowToolAssembler(&agentRunLineStub{})
	out := a.Assemble(context.Background(), workflowAgentTurn(nil, "oi"))
	if strings.Contains(out.Input.Messages[len(out.Input.Messages)-1].Content, agentRunHeading) {
		t.Fatalf("note = %q", out.Input.Messages[len(out.Input.Messages)-1].Content)
	}
}

func TestAContinuationTurnClosesTheNoteWithItsTriggerAfterTheRunLine(t *testing.T) {
	runs := &agentRunLineStub{line: `Fluxo "Matrícula" concluído há 1 min. Resultado do fluxo: {"status":"ok"}`}
	a := workflowToolAssembler(runs)
	req := workflowAgentTurn([]ai.Message{{Role: ai.RoleUser, Content: "segue meu CPF"}, {Role: ai.RoleAssistant, Content: "Vou verificar."}}, "")
	req.TriggerNote = `O fluxo "Matrícula" que você executou retornou agora.`

	out := a.Assemble(context.Background(), req)

	note := out.Input.Messages[len(out.Input.Messages)-1]
	if note.Role != ai.RoleUser || out.Input.VolatileTail != 1 {
		t.Fatalf("note = %+v, tail = %d", note, out.Input.VolatileTail)
	}
	runAt, triggerAt := strings.Index(note.Content, "Resultado do fluxo"), strings.Index(note.Content, "retornou agora")
	if runAt < 0 || triggerAt < runAt {
		t.Fatalf("the trigger must follow the run line:\n%s", note.Content)
	}
	if prev := out.Input.Messages[len(out.Input.Messages)-2]; prev.Content != "Vou verificar." {
		t.Fatalf("a continuation adds no customer message, got %+v", prev)
	}
}
