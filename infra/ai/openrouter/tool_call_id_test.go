package openrouter

import (
	"context"
	"sync"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

type callIDRecorder struct {
	injectFakeToolService
	mu  sync.Mutex
	ids []string
}

func (r *callIDRecorder) record(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, tools.CallIDFrom(ctx))
}

func (r *callIDRecorder) Execute(ctx context.Context, _ string, _ map[string]interface{}) (tools.ExecutionResult, error) {
	r.record(ctx)
	return tools.ExecutionResult{Result: "ok"}, nil
}

func (r *callIDRecorder) ExecuteWithConfig(ctx context.Context, _ string, _ map[string]interface{}, _ map[string]interface{}) (tools.ExecutionResult, error) {
	r.record(ctx)
	return tools.ExecutionResult{Result: "ok"}, nil
}

func TestEveryToolRunKnowsTheIDOfItsCall(t *testing.T) {
	srv, _, _ := scriptedServer(t, []string{toolCallReply, textReply})
	recorder := &callIDRecorder{}
	service := loopService(srv.URL)
	service.toolService = recorder
	_, err := service.Generate(context.Background(), ai.GenerateInput{
		Model:             "openai/gpt-4o",
		WorkspaceID:       "ws",
		Messages:          []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
		Tools:             []tools.Definition{{Name: "manage_entry_stage", Description: "etapa"}},
		ToolExecutionMode: ai.ToolExecutionModeAuto,
		ToolConfigs:       map[string]map[string]interface{}{"manage_entry_stage": {"__workspace_id": "ws"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.ids) != 1 || recorder.ids[0] != "c1" {
		t.Fatalf("call ids seen by the tool = %q", recorder.ids)
	}
}
