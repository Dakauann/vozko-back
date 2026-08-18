package tools_usecase

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"vozko/domain/tools"
)

// SimulationRunsForReal reports whether the agent simulator executes this tool
// for real instead of answering with a canned result.
//
// The rule is "pure retrieval only": a tool listed here may read anything but
// must not change our state, send to a person, or touch an external system in
// a way that sticks. Everything unlisted is stubbed, which is what makes the
// sandbox safe by default: a tool added next year is intercepted on day one,
// and the worst case is that someone has to add one line here to debug it.
//
// The knowledge is deliberately in ONE list rather than spread across the
// handlers as a per-tool flag. It is a safety boundary, and a boundary you can
// read top to bottom in five seconds is a boundary that stays correct.
//
// config carries the tool's operator configuration, because http_request
// cannot be judged by name alone: the same tool is a read when it is pointed
// at a GET endpoint and a write when it is pointed at a POST.
func SimulationRunsForReal(name string, config map[string]interface{}) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case SearchKnowledgeBaseToolName, CheckCalendarAvailabilityToolName, ValidateCEPToolName:
		return true
	case httpRequestToolName:
		method, _ := config["method"].(string)
		switch strings.ToUpper(strings.TrimSpace(method)) {
		case http.MethodGet, http.MethodHead:
			return true
		}
	}
	// Unlisted natives, every MCP tool, and any name the model invented.
	return false
}

// SimulatedToolService is the agent simulator's sandbox: it presents the real
// registry's definitions, so the agent sees its true tool set, dynamic enums
// and all, then routes each execution through SimulationRunsForReal.
//
// Wrapping the SERVICE (rather than flagging individual handlers) is what
// makes the guarantee total: there is exactly one code path from the simulator
// to a real Execute, and it runs the allowlist first.
//
// Retrieval runs for real because stubbing it was worse than useless: told
// "the search succeeded, carry on", the model answered the customer from its
// own weights, and the operator could not tell a bad query from an empty
// knowledge base.
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
// would bypass the allowlist, so the simulator's AI service must only ever
// call Execute/ExecuteWithConfig, which the OpenRouter adapter does.
func (s *SimulatedToolService) Handler(name string) (tools.Handler, bool) {
	if s.real == nil {
		return nil, false
	}
	return s.real.Handler(name)
}

func (s *SimulatedToolService) Execute(ctx context.Context, name string, params map[string]interface{}) (tools.ExecutionResult, error) {
	if s.real == nil || !SimulationRunsForReal(name, nil) {
		return s.stub(name), nil
	}
	return s.real.Execute(ctx, name, params)
}

func (s *SimulatedToolService) ExecuteWithConfig(ctx context.Context, name string, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	if s.real == nil || !SimulationRunsForReal(name, config) {
		return s.stub(name), nil
	}
	return s.real.ExecuteWithConfig(ctx, name, config, params)
}

// stub answers as a successful execution would, clearly marked, so the model
// carries the conversation forward exactly as it would in production. The
// interesting artifact, which tool with what arguments, surfaces on
// GenerateOutput.ToolCalls for the debug rail; the canned text only keeps the
// loop coherent.
func (s *SimulatedToolService) stub(name string) tools.ExecutionResult {
	log.Printf("[agent-simulator] intercepted tool call %q (no side effects)", name)
	return tools.ExecutionResult{
		Result: fmt.Sprintf(
			"(SIMULAÇÃO) A ferramenta %q teria sido executada com os parâmetros informados; nenhuma ação real foi realizada. Considere a ação concluída com sucesso e continue a conversa normalmente.",
			name,
		),
	}
}

var _ tools.Service = (*SimulatedToolService)(nil)
