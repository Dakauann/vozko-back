package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/agentloop"
)

type AccessChecker interface {
	Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error
}

type IDGenerator func() string

const (
	EventChart     = "chart"
	EventToolStart = "tool_start"
)

type FundsChecker interface {
	Check(workspaceID string) error
}

var ErrFundsExhausted = errors.New("copilot: funds exhausted")

type Driver struct {
	cc       copilot.Context
	model    string
	registry *Registry
	access   AccessChecker
	funds    FundsChecker
	newID    IDGenerator
}

func NewDriver(cc copilot.Context, model string, reg *Registry, access AccessChecker, funds FundsChecker, newID IDGenerator) *Driver {
	return &Driver{cc: cc, model: model, registry: reg, access: access, funds: funds, newID: newID}
}

func (d *Driver) Admit(context.Context) error {
	if d.funds == nil {
		return ErrFundsExhausted
	}
	if err := d.funds.Check(d.cc.WorkspaceID); err != nil {
		return fmt.Errorf("%w: %v", ErrFundsExhausted, err)
	}
	return nil
}

func (d *Driver) Model() string             { return d.model }
func (d *Driver) Tools() []tools.Definition { return d.registry.Definitions() }

func (d *Driver) SystemPrompt() string { return systemPrompt(d.cc.View, time.Now()) }

func (d *Driver) Reground(iter, maxIter, noMutationStreak int) string {
	return "OBSERVAÇÃO DO SISTEMA (não é uma nova pergunta): use ferramentas quando úteis; conclua respondendo ao usuário."
}

func (d *Driver) Refresh()                   {}
func (d *Driver) AfterTurn(_ agentloop.Emit) {}
func (d *Driver) Progress() agentloop.Progress {
	return agentloop.Progress{Valid: true}
}
func (d *Driver) FinishVerdict(call ai.ToolCall) agentloop.FinishResult {
	summary, _ := call.Arguments["summary"].(string)
	if summary == "" {
		summary = "concluído"
	}
	return agentloop.FinishResult{Honored: true, Summary: summary, Result: "ok"}
}

func (d *Driver) Dispatch(ctx context.Context, call ai.ToolCall, emit agentloop.Emit) agentloop.StepResult {
	tool, ok := d.registry.Get(call.Name)
	if !ok {
		return agentloop.StepResult{Result: "ferramenta desconhecida: " + call.Name}
	}
	m := tool.Meta()
	if err := d.access.Execute(d.cc.UserID, d.cc.WorkspaceID, m.Resource, m.Action); err != nil {
		emit("tool", toolEvent(call.Name, string(copilot.StatusDenied), false))
		return agentloop.StepResult{Result: fmt.Sprintf(
			"PERMISSÃO NEGADA: o usuário não tem permissão para %s:%s neste workspace. Não tente contornar.",
			m.Resource, m.Action)}
	}
	if m.Mutating {
		pa := copilot.PendingAction{
			ID:       d.mintID(),
			ToolName: call.Name,
			Args:     call.Arguments,
			Summary:  summarizeCall(call),
		}
		emit("tool_proposal", pa)
		return agentloop.StepResult{
			Result: "AÇÃO PROPOSTA e aguardando aprovação explícita do usuário. NÃO presuma que foi concluída.",
			Pause:  &agentloop.Pause{Reason: "aguardando aprovação", Payload: pa},
		}
	}
	emit(EventToolStart, map[string]interface{}{"name": call.Name})
	res := tool.Execute(ctx, d.cc, call.Arguments)
	emit("tool", toolEvent(call.Name, string(res.Status), res.Status == copilot.StatusOK))
	if res.Chart != nil {
		emit(EventChart, res.Chart)
	}
	return agentloop.StepResult{Result: renderResult(res)}
}

func (d *Driver) ExecuteApproved(ctx context.Context, pa copilot.PendingAction) copilot.Result {
	tool, ok := d.registry.Get(pa.ToolName)
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: "ferramenta desconhecida: " + pa.ToolName}
	}
	m := tool.Meta()
	if err := d.access.Execute(d.cc.UserID, d.cc.WorkspaceID, m.Resource, m.Action); err != nil {
		return copilot.Result{Status: copilot.StatusDenied, Message: "permissão negada"}
	}
	return tool.Execute(ctx, d.cc, pa.Args)
}

func (d *Driver) mintID() string {
	if d.newID != nil {
		return d.newID()
	}
	return "act"
}

func DefaultConfig(cc copilot.Context, tokenBudget int) agentloop.Config {
	return agentloop.Config{
		WorkspaceID:        cc.WorkspaceID,
		Temperature:        0.2,
		MaxTokensPerGen:    4000,
		MaxIterations:      12,
		SessionTokenBudget: tokenBudget,
		FinishToolName:     "finish",
		LogPrefix:          "[copilot] ws=" + cc.WorkspaceID,
	}
}

func toolEvent(name, summary string, ok bool) map[string]interface{} {
	return map[string]interface{}{"name": name, "summary": summary, "ok": ok}
}

func summarizeCall(call ai.ToolCall) string {
	b, err := json.Marshal(call.Arguments)
	if err != nil {
		return call.Name
	}
	return call.Name + " " + string(b)
}

func renderResult(r copilot.Result) string {
	if r.Status != copilot.StatusOK {
		if r.Message != "" {
			return string(r.Status) + ": " + r.Message
		}
		return string(r.Status)
	}
	if r.Data == nil {
		return "ok"
	}
	b, err := json.Marshal(r.Data)
	if err != nil {
		return "ok"
	}
	return string(b)
}
