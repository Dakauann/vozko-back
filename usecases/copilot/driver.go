package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"vozko/domain/readiness"

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
	EventCard      = "action_card"
	EventImage     = "image"
	EventToolStart = "tool_start"
)

type FundsChecker interface {
	Check(workspaceID string) error
}

var ErrFundsExhausted = errors.New("copilot: funds exhausted")

var ErrNoValidator = errors.New("esta ação não tem verificação prévia e não pode ser proposta")

type Driver struct {
	cc       copilot.Context
	model    string
	registry *Registry
	access   AccessChecker
	funds    FundsChecker
	newID    IDGenerator
	state    *readiness.Snapshot
	offered  []tools.Definition
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

func (d *Driver) Model() string { return d.model }
func (d *Driver) Tools() []tools.Definition {
	if d.offered == nil {
		d.offered = d.permittedDefinitions()
	}
	return d.offered
}

func (d *Driver) permittedDefinitions() []tools.Definition {
	defs := make([]tools.Definition, 0)
	for _, t := range d.registry.Tools() {
		if d.permit(t.Meta()) == nil {
			defs = append(defs, t.Definition())
		}
	}
	return defs
}

func (d *Driver) SystemPrompt() string {
	return systemPrompt(d.cc.View, time.Now()) + workspacePrompt(d.state)
}

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
	if err := d.permit(m); err != nil {
		emit("tool", toolStep{Name: call.Name, Summary: string(copilot.StatusDenied)}.payload())
		return agentloop.StepResult{Result: fmt.Sprintf(
			"PERMISSÃO NEGADA: o usuário não tem permissão para %s:%s neste workspace. Não tente contornar.",
			m.Resource, m.Action)}
	}
	if m.Mutating {
		secrets := secretsOf(tool, call.Arguments)
		call.Arguments = copilot.Conceal(tool, call.Arguments, secrets)
		if err := preflight(ctx, tool, d.cc, call.Arguments); err != nil {
			emit("tool", toolStep{Name: call.Name, Summary: string(copilot.StatusError)}.payload())
			return agentloop.StepResult{Result: fmt.Sprintf(
				"PROPOSTA RECUSADA ANTES DE CHEGAR AO USUÁRIO: %v. Confira os dados com as ferramentas de leitura e proponha de novo; nunca invente ids.", err)}
		}
		pa := copilot.PendingAction{
			ID:       d.mintID(),
			ToolName: call.Name,
			Args:     call.Arguments,
			Summary:  summarizeCall(call),
			Fields:   describe(ctx, tool, d.cc, call.Arguments),
			Preview:  preview(ctx, tool, d.cc, call.Arguments),
			Secrets:  secrets,
		}
		emit("tool_proposal", pa)
		return agentloop.StepResult{
			Result: "AÇÃO PROPOSTA e aguardando aprovação explícita do usuário. NÃO presuma que foi concluída.",
			Pause:  &agentloop.Pause{Reason: "aguardando aprovação", Payload: pa},
		}
	}
	res := d.run(ctx, tool, call.Name, call.Arguments, emit)
	emitStep(emit, stepFromResult(call.Name, res))
	return agentloop.StepResult{Result: renderResult(res)}
}

func (d *Driver) ExecuteApproved(ctx context.Context, pa copilot.PendingAction, provided map[string]string, emit agentloop.Emit) copilot.Result {
	tool, ok := d.registry.Get(pa.ToolName)
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: "ferramenta desconhecida: " + pa.ToolName}
	}
	m := tool.Meta()
	if err := d.permit(m); err != nil {
		return copilot.Result{Status: copilot.StatusDenied, Message: "permissão negada"}
	}
	if err := preflight(ctx, tool, d.cc, pa.Args); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "a mudança não passou mais na verificação e nada foi alterado: " + err.Error()}
	}
	args, err := copilot.WithSecrets(pa.Args, secretsOf(tool, pa.Args), provided)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "o usuário aprovou sem preencher o campo protegido (" + err.Error() + "); nada foi alterado. Proponha de novo e peça que ele preencha o campo no cartão de aprovação, nunca no chat."}
	}
	return d.run(ctx, tool, pa.ToolName, args, emit)
}

func (d *Driver) run(ctx context.Context, tool copilot.Tool, name string, args map[string]interface{}, emit agentloop.Emit) copilot.Result {
	emit(EventToolStart, map[string]interface{}{"name": name})
	return tool.Execute(ctx, d.cc, args)
}

func secretsOf(tool copilot.Tool, args map[string]interface{}) []copilot.SecretField {
	if asker, ok := tool.(copilot.SecretAsker); ok {
		return asker.Secrets(args)
	}
	return nil
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

func (d *Driver) permit(m copilot.Meta) error {
	if d.cc.SystemAdmin {
		return nil
	}
	return d.access.Execute(d.cc.UserID, d.cc.WorkspaceID, m.Resource, m.Action)
}

func preflight(ctx context.Context, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) error {
	validator, ok := tool.(copilot.Validator)
	if !ok {
		return ErrNoValidator
	}
	return validator.Validate(ctx, cc, args)
}

func preview(ctx context.Context, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	if previewer, ok := tool.(copilot.Previewer); ok {
		return previewer.Preview(ctx, cc, args)
	}
	return nil
}

func describe(ctx context.Context, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	if describer, ok := tool.(copilot.Describer); ok {
		return describer.Describe(ctx, cc, args)
	}
	return copilot.DescribeArgs(args)
}
