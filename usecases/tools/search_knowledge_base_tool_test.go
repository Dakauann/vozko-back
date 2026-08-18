package tools_usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/agent"
	"vozko/domain/rag"
	"vozko/domain/tools"
	"vozko/usecases/agentctx"
)

type ragServiceStub struct {
	lastInput rag.AgentQueryInput
	results   []rag.QueryResult
	err       error
	calls     int
}

func (s *ragServiceStub) Query(ctx context.Context, in rag.QueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{}, nil
}

func (s *ragServiceStub) QueryForAgent(ctx context.Context, in rag.AgentQueryInput) (*rag.QueryOutput, error) {
	s.calls++
	s.lastInput = in
	if s.err != nil {
		return nil, s.err
	}
	return &rag.QueryOutput{Results: s.results, TotalFound: len(s.results)}, nil
}

func TestSearchKnowledgeBaseTool_Success(t *testing.T) {
	st := &ragServiceStub{results: []rag.QueryResult{
		{DocumentName: "precos.md", Content: "Plano anual: R$ 500", Score: 0.91},
		{Content: "Sem fonte", Score: 0.42},
	}}
	kt := NewSearchKnowledgeBaseToolUseCase(st).(*searchKnowledgeBaseTool)
	out, err := kt.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__agent_id": "ag-1",
	}, map[string]interface{}{"query": "preço do plano anual"})
	require.NoError(t, err)
	require.False(t, out.IsError)
	require.Equal(t, 1, st.calls)
	require.Equal(t, "ag-1", st.lastInput.AgentID)
	require.Equal(t, "preço do plano anual", st.lastInput.Query)
	require.True(t, st.lastInput.IncludeMetadata)
	// Defaults from RAGConfig.WithDefaults when no context agent is present.
	require.Equal(t, 5, st.lastInput.TopK)
	require.InDelta(t, 0.3, st.lastInput.MinScore, 0.001)
	text, ok := out.Result.(string)
	require.True(t, ok)
	require.Contains(t, text, "[1] Fonte: precos.md")
	require.Contains(t, text, "Plano anual: R$ 500")
	require.Contains(t, text, "[2]")
}

func TestSearchKnowledgeBaseTool_AgentFromContext(t *testing.T) {
	st := &ragServiceStub{results: []rag.QueryResult{{Content: "x", Score: 0.5}}}
	kt := NewSearchKnowledgeBaseToolUseCase(st).(*searchKnowledgeBaseTool)
	ctx := agentctx.WithAgent(context.Background(), &agent.Agent{
		ID:        "ag-ctx",
		RAGConfig: &agent.RAGConfig{MaxChunks: 8, MinScore: 0.6},
	})
	out, err := kt.ExecuteWithConfig(ctx, nil, map[string]interface{}{"query": "horário de atendimento"})
	require.NoError(t, err)
	require.False(t, out.IsError)
	require.Equal(t, "ag-ctx", st.lastInput.AgentID)
	require.Equal(t, 8, st.lastInput.TopK)
	require.InDelta(t, 0.6, st.lastInput.MinScore, 0.001)
}

func TestSearchKnowledgeBaseTool_EmptyResults(t *testing.T) {
	st := &ragServiceStub{}
	kt := NewSearchKnowledgeBaseToolUseCase(st).(*searchKnowledgeBaseTool)
	out, err := kt.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__agent_id": "ag-1",
	}, map[string]interface{}{"query": "assunto inexistente"})
	require.NoError(t, err)
	require.False(t, out.IsError)
	require.Contains(t, out.Result, "Nenhum resultado")
}

func TestSearchKnowledgeBaseTool_MissingQuery(t *testing.T) {
	st := &ragServiceStub{}
	kt := NewSearchKnowledgeBaseToolUseCase(st).(*searchKnowledgeBaseTool)
	out, err := kt.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__agent_id": "ag-1",
	}, map[string]interface{}{"query": "   "})
	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Equal(t, 0, st.calls)
}

func TestSearchKnowledgeBaseTool_MissingAgent(t *testing.T) {
	st := &ragServiceStub{}
	kt := NewSearchKnowledgeBaseToolUseCase(st).(*searchKnowledgeBaseTool)
	out, err := kt.ExecuteWithConfig(context.Background(), nil, map[string]interface{}{"query": "preço"})
	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Equal(t, 0, st.calls)
}

func TestSearchKnowledgeBaseTool_ServiceError(t *testing.T) {
	st := &ragServiceStub{err: errors.New("boom")}
	kt := NewSearchKnowledgeBaseToolUseCase(st).(*searchKnowledgeBaseTool)
	out, err := kt.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__agent_id": "ag-1",
	}, map[string]interface{}{"query": "preço"})
	require.NoError(t, err)
	require.True(t, out.IsError)
	require.Equal(t, 1, st.calls)
}

func TestSearchKnowledgeBaseTool_NilService(t *testing.T) {
	require.Nil(t, NewSearchKnowledgeBaseToolUseCase(nil))
}

func TestSearchKnowledgeBaseTool_Definition(t *testing.T) {
	kt := NewSearchKnowledgeBaseToolUseCase(&ragServiceStub{}).(*searchKnowledgeBaseTool)
	def := kt.Definition()
	require.Equal(t, SearchKnowledgeBaseToolName, def.Name)
	require.Contains(t, def.Required, "query")
	require.Contains(t, def.Parameters, "query")
	require.Contains(t, def.Visibility, tools.VisibilityMessaging)
	require.Equal(t, tools.CategoryAgentUtility, def.Category)
}
