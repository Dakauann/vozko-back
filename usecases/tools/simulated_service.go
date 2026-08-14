package tools_usecase

import (
	"context"
	"fmt"
	"log"

	"vozko/domain/tools"
)

// SimulatedToolService is the agent simulator's sandbox: it presents the real
// registry's definitions: the agent sees its true tool set, dynamic enums and
// all, but intercepts every execution and answers with a canned result. No
// CRM write, no message send, no HTTP request, no MCP call ever happens
// through it.
//
// Wrapping the SERVICE (rather than flagging individual handlers) is what
// makes the guarantee total: a tool added next year is sandboxed on day one,
// because there is no code path from the simulator to a real Execute.
type SimulatedToolService struct {
	real tools.Service
}

func NewSimulatedToolService(real tools.Service) *SimulatedToolService {
	return &SimulatedToolService{real: real}
}

func (s *SimulatedToolService) Definitions() []tools.Definition {
	if s.real == nil {
		return nil
	}
	return s.real.Definitions()
}

func (s *SimulatedToolService) DefinitionsFor(v tools.ToolVisibility) []tools.Definition {
	if s.real == nil {
		return nil
	}
	return s.real.DefinitionsFor(v)
}

// Handler exposes the real handler for definition lookups only. Executing it
// would escape the sandbox, so the simulator's AI service must only ever call
// Execute/ExecuteWithConfig, which the OpenRouter adapter does.
func (s *SimulatedToolService) Handler(name string) (tools.Handler, bool) {
	if s.real == nil {
		return nil, false
	}
	return s.real.Handler(name)
}

func (s *SimulatedToolService) Execute(ctx context.Context, name string, params map[string]interface{}) (tools.ExecutionResult, error) {
	return s.simulate(name), nil
}

func (s *SimulatedToolService) ExecuteWithConfig(ctx context.Context, name string, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	return s.simulate(name), nil
}

// simulate answers as a successful execution would, clearly marked, so the
// model carries the conversation forward exactly as it would in production.
// The interesting artifact, which tool with what arguments, surfaces on
// GenerateOutput.ToolCalls for the debug rail; the canned text only keeps the
// loop coherent.
func (s *SimulatedToolService) simulate(name string) tools.ExecutionResult {
	log.Printf("[agent-simulator] intercepted tool call %q (no side effects)", name)
	return tools.ExecutionResult{
		Result: fmt.Sprintf(
			"(SIMULAÇÃO) A ferramenta %q teria sido executada com os parâmetros informados; nenhuma ação real foi realizada. Considere a ação concluída com sucesso e continue a conversa normalmente.",
			name,
		),
	}
}

var _ tools.Service = (*SimulatedToolService)(nil)
