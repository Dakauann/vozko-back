package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
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
	cc.Model = model
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
		if d.cc.View.Offers(t) && d.permitTool(t) == nil {
			defs = append(defs, t.Definition())
		}
	}
	return defs
}

func (d *Driver) SystemPrompt() string {
	return systemPrompt(d.cc.View, time.Now())
}

func (d *Driver) Reground(iter, maxIter, noMutationStreak int) string {
	return "OBSERVAÇÃO DO SISTEMA (não é uma nova pergunta): use ferramentas quando úteis; conclua respondendo ao usuário." + workspacePrompt(d.state)
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
	if !d.cc.View.Offers(tool) {
		emit("tool", toolStep{Name: call.Name, Summary: string(copilot.StatusDenied)}.payload())
		return agentloop.StepResult{Result: "FERRAMENTA INDISPONÍVEL NESTA TELA: " + call.Name + " só funciona com o usuário na tela própria dela."}
	}
	m := tool.Meta()
	if err := d.permitTool(tool); err != nil {
		emit("tool", toolStep{Name: call.Name, Summary: string(copilot.StatusDenied)}.payload())
		return agentloop.StepResult{Result: fmt.Sprintf(
			"PERMISSÃO NEGADA: o usuário não tem permissão para %s:%s neste workspace. Não tente contornar.",
			m.Resource, m.Action)}
	}
	if copilot.NeedsApproval(tool, d.cc.Mode) {
		return d.propose(ctx, tool, call, emit)
	}
	args := call.Arguments
	if m.Mutating {
		auto, ok, err := d.autoApproved(ctx, tool, args)
		if err != nil {
			emit("tool", toolStep{Name: call.Name, Summary: string(copilot.StatusError)}.payload())
			return agentloop.StepResult{Result: fmt.Sprintf("AÇÃO RECUSADA ANTES DE EXECUTAR: %v. Confira os dados com as ferramentas de leitura e tente de novo; nunca invente ids.", err)}
		}
		if !ok {
			return d.propose(ctx, tool, call, emit)
		}
		args = auto
	}
	res := d.run(ctx, tool, call.Name, args, emit)
	if res.Status != copilot.StatusOK {
		log.Printf("[copilot] ws=%s tool=%s failed: %s", d.cc.WorkspaceID, call.Name, clippedError(res.Message))
		if logged := failedArguments(tool, args); logged != "" {
			log.Printf("[copilot] ws=%s tool=%s arguments: %s", d.cc.WorkspaceID, call.Name, logged)
		}
	}
	emitStep(emit, stepFromResult(call.Name, res))
	step := agentloop.StepResult{Result: renderResult(res), EndTurn: res.EndTurn}
	if res.Status == copilot.StatusOK {
		step.Images = res.Images
	}
	return step
}

func (d *Driver) propose(ctx context.Context, tool copilot.Tool, call ai.ToolCall, emit agentloop.Emit) agentloop.StepResult {
	choices, err := choicesOf(ctx, tool, d.cc, call.Arguments)
	if err != nil {
		emit("tool", toolStep{Name: call.Name, Summary: string(copilot.StatusError)}.payload())
		return agentloop.StepResult{Result: fmt.Sprintf(
			"PROPOSTA RECUSADA ANTES DE CHEGAR AO USUÁRIO: as opções do cartão de aprovação não puderam ser carregadas (%v). Avise o usuário e tente de novo em instantes.", err)}
	}
	call.Arguments = copilot.StripChoices(call.Arguments, choices)
	secrets := secretsOf(tool, call.Arguments)
	call.Arguments = copilot.Conceal(tool, call.Arguments, secrets)
	cc := d.proposalContext(d.mintID())
	if err := preflight(ctx, tool, cc, call.Arguments); err != nil {
		emit("tool", toolStep{Name: call.Name, Summary: string(copilot.StatusError)}.payload())
		return agentloop.StepResult{Result: fmt.Sprintf(
			"PROPOSTA RECUSADA ANTES DE CHEGAR AO USUÁRIO: %v. Confira os dados com as ferramentas de leitura e proponha de novo; nunca invente ids.", err)}
	}
	pa := copilot.PendingAction{
		ID:       cc.ProposalID,
		ToolName: call.Name,
		Args:     call.Arguments,
		Summary:  summarizeCall(call),
		Fields:   describe(ctx, tool, cc, call.Arguments),
		Preview:  preview(ctx, tool, cc, call.Arguments),
		Secrets:  secrets,
		Choices:  choices,
	}
	emit("tool_proposal", pa)
	return agentloop.StepResult{
		Result: "AÇÃO PROPOSTA e aguardando aprovação explícita do usuário. NÃO presuma que foi concluída.",
		Pause:  &agentloop.Pause{Reason: "aguardando aprovação", Payload: pa},
	}
}

func (d *Driver) autoApproved(ctx context.Context, tool copilot.Tool, args map[string]interface{}) (map[string]interface{}, bool, error) {
	if _, asksSecrets := tool.(copilot.SecretAsker); asksSecrets {
		return nil, false, nil
	}
	choices, err := choicesOf(ctx, tool, d.cc, args)
	if err != nil {
		return nil, false, nil
	}
	defaults := make(map[string]string, len(choices))
	for _, choice := range choices {
		if strings.TrimSpace(choice.Default) == "" {
			return nil, false, nil
		}
		defaults[choice.Key] = choice.Default
	}
	chosen, err := copilot.WithChoices(args, choices, defaults)
	if err != nil {
		return nil, false, nil
	}
	if err := preflight(ctx, tool, d.cc, chosen); err != nil {
		return nil, false, err
	}
	return chosen, true, nil
}

func (d *Driver) ExecuteApproved(ctx context.Context, pa copilot.PendingAction, approval copilot.Approval, emit agentloop.Emit) copilot.Result {
	tool, ok := d.registry.Get(pa.ToolName)
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: "ferramenta desconhecida: " + pa.ToolName}
	}
	if !d.cc.View.Offers(tool) {
		return copilot.Result{Status: copilot.StatusDenied, Message: "esta ação só pode ser executada na tela própria dela"}
	}
	if err := d.permitTool(tool); err != nil {
		return copilot.Result{Status: copilot.StatusDenied, Message: "permissão negada"}
	}
	cc := d.proposalContext(pa.ID)
	choices, err := choicesOf(ctx, tool, cc, pa.Args)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "as opções do cartão de aprovação não puderam ser carregadas e nada foi alterado: " + err.Error()}
	}
	chosen, err := copilot.WithChoices(pa.Args, choices, approval.Choices)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "o usuário aprovou sem fazer a escolha do cartão (" + err.Error() + "); nada foi alterado. Proponha de novo e peça que ele escolha no cartão de aprovação."}
	}
	if err := preflight(ctx, tool, cc, chosen); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "a mudança não passou mais na verificação e nada foi alterado: " + err.Error()}
	}
	args, err := copilot.WithSecrets(chosen, secretsOf(tool, chosen), approval.Secrets)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "o usuário aprovou sem preencher o campo protegido (" + err.Error() + "); nada foi alterado. Proponha de novo e peça que ele preencha o campo no cartão de aprovação, nunca no chat."}
	}
	return d.runAs(ctx, cc, tool, pa.ToolName, args, emit)
}

func (d *Driver) proposalContext(id string) copilot.Context {
	cc := d.cc
	cc.ProposalID = id
	return cc
}

func (d *Driver) run(ctx context.Context, tool copilot.Tool, name string, args map[string]interface{}, emit agentloop.Emit) copilot.Result {
	return d.runAs(ctx, d.cc, tool, name, args, emit)
}

func (d *Driver) runAs(ctx context.Context, cc copilot.Context, tool copilot.Tool, name string, args map[string]interface{}, emit agentloop.Emit) copilot.Result {
	emit(EventToolStart, map[string]interface{}{"name": name})
	return tool.Execute(ctx, cc, args)
}

func choicesOf(ctx context.Context, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) ([]copilot.ChoiceField, error) {
	if asker, ok := tool.(copilot.ChoiceAsker); ok {
		return asker.Choices(ctx, cc, args)
	}
	return nil, nil
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

func DefaultConfig(cc copilot.Context, model ai.ModelInfo, costCeilingMicros int64) agentloop.Config {
	cfg := agentloop.Config{
		WorkspaceID:      cc.WorkspaceID,
		BillingReference: cc.ChargeReference,
		SessionID:        cc.ChargeReference,
		Temperature:      0.2,
		MaxTokensPerGen:  answerMaxTokensPerGen,
		MaxIterations:    answerMaxIterations,
		FinishToolName:   "finish",
		LogPrefix:        "[copilot] ws=" + cc.WorkspaceID,
		GraceInstruction: answerGraceInstruction,
	}
	if cc.View.Focused() {
		cfg.MaxTokensPerGen = studioMaxTokensPerGen
		cfg.ReasoningMaxTokens = studioReasoningMaxTokens
	}
	if model.HasKnownLimits() && costCeilingMicros > 0 {
		cfg.ModelLimits, cfg.CostCeilingMicros = model, costCeilingMicros
		return cfg
	}
	cfg.SessionTokenBudget = AnswerTokenBudget
	return cfg
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

type Prerequisites interface {
	AlsoRequires() []workspace.PermissionEntry
}

func (d *Driver) permitTool(tool copilot.Tool) error {
	if err := d.permit(tool.Meta()); err != nil {
		return err
	}
	extra, ok := tool.(Prerequisites)
	if !ok {
		return nil
	}
	for _, p := range extra.AlsoRequires() {
		if err := d.permit(copilot.Meta{Resource: p.Resource, Action: p.Action}); err != nil {
			return err
		}
	}
	return nil
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
