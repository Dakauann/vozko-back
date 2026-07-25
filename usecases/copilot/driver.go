package copilot_usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/agentloop"
)

// AccessChecker is the RBAC gate. workspace.CheckAccessUseCase satisfies it; the
// copilot depends on this narrow interface (downward dep on workspace types only)
// so it is trivially testable.
type AccessChecker interface {
	Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error
}

// IDGenerator mints pending-action ids. Injected so tests stay deterministic.
type IDGenerator func() string

// Driver adapts one copilot session to the generic agentloop.Driver seam. Reads
// execute immediately (after an RBAC check); mutations are RBAC-checked and then
// PAUSED for explicit user approval — the engine yields, and ExecuteApproved runs
// the real usecase once the user confirms. Scope is taken from the session
// Context, never from model arguments.
type Driver struct {
	cc       copilot.Context
	model    string
	registry *Registry
	access   AccessChecker
	newID    IDGenerator
}

// NewDriver builds a copilot driver for one authenticated session.
func NewDriver(cc copilot.Context, model string, reg *Registry, access AccessChecker, newID IDGenerator) *Driver {
	return &Driver{cc: cc, model: model, registry: reg, access: access, newID: newID}
}

func (d *Driver) Model() string             { return d.model }
func (d *Driver) Tools() []tools.Definition { return d.registry.Definitions() }

func (d *Driver) SystemPrompt() string { return systemPrompt() }

func (d *Driver) Reground(prompt string, iter, maxIter, noMutationStreak int) string {
	return "Pedido do usuário: " + prompt + "\nUse ferramentas quando úteis; conclua respondendo ao usuário."
}

// Reads and mutations carry no server-side mutable state, so the stall guards are
// inert (empty hash/signature) and finish is always allowed — the copilot ends a
// turn by replying conversationally (the engine's idle path) or pausing.
func (d *Driver) Refresh()                {}
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

// Dispatch routes one tool call: unknown → error; RBAC-denied → denied (fed back,
// no execution); mutating → propose + pause; read → execute now.
func (d *Driver) Dispatch(ctx context.Context, call ai.ToolCall, emit agentloop.Emit) agentloop.StepResult {
	tool, ok := d.registry.Get(call.Name)
	if !ok {
		return agentloop.StepResult{Result: "ferramenta desconhecida: " + call.Name}
	}
	m := tool.Meta()
	if err := d.access.Execute(d.cc.UserID, d.cc.WorkspaceID, m.Resource, m.Action); err != nil {
		emit("tool", toolEvent(call.Name, "permissão negada", false))
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
	res := tool.Execute(ctx, d.cc, call.Arguments)
	emit("tool", toolEvent(call.Name, string(res.Status), res.Status == copilot.StatusOK))
	return agentloop.StepResult{Result: renderResult(res)}
}

// ExecuteApproved runs a previously-proposed mutation after the user approves it.
// It re-checks RBAC (permissions may have changed since the proposal) and then
// calls the real usecase via the tool — so domain validation still applies.
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

// DefaultConfig returns the agentloop tuning for a copilot session. The stall
// guards are inert (the copilot has no validator); termination is the model's
// conversational reply (idle) or the iteration/token bounds.
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

// ---- helpers -------------------------------------------------------------

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

