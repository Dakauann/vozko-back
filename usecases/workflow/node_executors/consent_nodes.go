package node_executors

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/campaign"
	"vozko/domain/consentevidence"
	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	lead_usecase "vozko/usecases/lead"
)

const consentRunNotePrefix = "workflow_run:"

var errConsentGrantWithoutReply = fmt.Errorf("%w: this run has no message from the lead to cite", lead_domain.ErrConsentEvidenceRequired)

type ConsentRecorder interface {
	Record(ctx context.Context, workspaceID, leadID string, change lead_domain.ConsentChange) (lead_usecase.ConsentOutcome, error)
}

type CitedMessageReader interface {
	GetByID(id string) (*conversation.Message, error)
}

type ConsentNodeDeps struct {
	Consent    ConsentRecorder
	Messages   CitedMessageReader
	EntryLeads lead_domain.EntryLeads
	Leads      leadLookup
}

type consentExecutor struct{ deps ConsentNodeDeps }

type checkConsentExecutor struct{ deps ConsentNodeDeps }

func NewConsentExecutor(deps ConsentNodeDeps) workflow.NodeExecutor {
	return &consentExecutor{deps: deps}
}

func NewCheckConsentExecutor(deps ConsentNodeDeps) workflow.NodeExecutor {
	return &checkConsentExecutor{deps: deps}
}

func (e *consentExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionConsent,
		TestBehavior: workflow.NodeTestWritesData,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Consentimento",
		Description:  "Registra ou retira o consentimento do lead da conversa para receber mensagens.",
		Icon:         "ShieldCheck",
		Guidance: workflow.NodeGuidance{
			When: "Para gravar no lead da conversa a vontade dele sobre receber mensagens: 'registrar' quando ele aceitou, 'retirar' quando pediu para parar.",
			Behavior: "Modo 'registrar' só é aceito depois da resposta do lead em todos os caminhos (saída 'replied' de wait_for_reply, uma opção de action_send_interactive " +
				"ou a saída 'true' de uma condição sobre {{selected_option_id}} ou {{selected_option_title}}) e cita a mensagem do lead que acordou o fluxo, " +
				"inclusive o toque em um botão de template. Modo 'retirar' vale em qualquer ponto: cita a mensagem que disparou o fluxo " +
				"ou, sem mensagem, grava a nota do nó com a execução. Depois de retirar, os envios do fluxo para este lead são recusados. " +
				"Qualquer recusa ou falha interrompe a execução com o motivo; o fluxo fica registrado como autor.",
			Examples: []string{
				"send_template → wait_for_reply; condition_branch {{selected_option_id}} eq template_button:0 → action_consent mode=registrar",
				"condition_text_match 'parar' → action_consent mode=retirar",
			},
		},
		Outputs: []workflow.HandleDefinition{{ID: "sucesso", Label: "Registrado"}},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "lead_id", Description: "ID do lead da conversa", Label: "ID do lead", Type: workflow.VariableTypeText},
			{Key: "changed", Description: "true quando a vontade do lead mudou agora; false quando já estava registrada", Label: "Mudou agora", Type: workflow.VariableTypeBoolean},
			{Key: "event_id", Description: "ID do evento na linha do tempo do lead, quando mudou", Label: "ID do evento", Type: workflow.VariableTypeText},
		},
		DefaultConfig: map[string]interface{}{workflow.ConsentModeKey: "", workflow.ConsentPurposeKey: "", workflow.ConsentNoteKey: ""},
		ConfigSchema: []workflow.ConfigField{
			{Key: workflow.ConsentModeKey, Label: "Ação", Type: "select", Required: true, Options: []workflow.ConfigFieldOption{
				{Value: workflow.ConsentModeGrant, Label: "Registrar consentimento"},
				{Value: workflow.ConsentModeWithdraw, Label: "Retirar consentimento (parar de receber)"},
			}},
			{Key: workflow.ConsentPurposeKey, Label: "Finalidade", Type: "text", Placeholder: "receber ofertas pelo WhatsApp",
				Description: "Opcional. Para que o lead aceitou receber mensagens. Aceita variáveis.",
				VisibleWhen: &workflow.FieldRule{Field: workflow.ConsentModeKey, Values: []string{workflow.ConsentModeGrant}}},
			{Key: workflow.ConsentNoteKey, Label: "Nota", Type: "text", Placeholder: "pedido recebido por telefone",
				Description: "Opcional. Gravada como prova quando a execução não tem uma mensagem do lead. Aceita variáveis.",
				VisibleWhen: &workflow.FieldRule{Field: workflow.ConsentModeKey, Values: []string{workflow.ConsentModeWithdraw}}},
		},
	}
}

func (e *checkConsentExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeConditionConsent,
		TestBehavior: workflow.NodeTestReadsData,
		Category:     workflow.NodeCategoryCondition,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Lead tem consentimento?",
		Description:  "Segue pelo estado do consentimento do lead da conversa: concedido, retirado ou sem registro.",
		Icon:         "SealQuestion",
		Guidance: workflow.NodeGuidance{
			When:     "Para seguir caminhos diferentes conforme o lead da conversa aceitou receber mensagens, pediu para parar ou não tem registro.",
			Behavior: "Lê o registro atual do lead. Uma conversa sem lead segue por 'none'. Uma falha de leitura interrompe a execução em vez de escolher um caminho.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: workflow.ConsentBranchGranted, Label: "Tem consentimento"},
			{ID: workflow.ConsentBranchWithdrawn, Label: "Pediu para não receber"},
			{ID: workflow.ConsentBranchNone, Label: "Sem registro"},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "state", Description: "granted, withdrawn ou none", Label: "Situação do consentimento", Type: workflow.VariableTypeText},
			{Key: "lead_id", Description: "ID do lead da conversa (vazio se não há)", Label: "ID do lead", Type: workflow.VariableTypeText},
		},
		DefaultConfig: map[string]interface{}{},
	}
}

func (e *consentExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if e.deps.Consent == nil || e.deps.Messages == nil || e.deps.EntryLeads == nil {
		return consentStepFailure("o registro de consentimento está indisponível neste contexto (ex.: simulação)"), nil
	}
	mode := nodeText(ctx, workflow.ConsentModeKey)
	if mode != workflow.ConsentModeGrant && mode != workflow.ConsentModeWithdraw {
		return consentStepFailure("escolha a ação do nó: registrar ou retirar"), nil
	}
	leadID, err := e.deps.EntryLeads.LeadOfEntry(context.Background(), ctx.Run.WorkspaceID, runEntry(ctx.Run))
	if errors.Is(err, lead_domain.ErrLeadNotFound) {
		return codedConsentFailure(err, "a conversa não tem um lead"), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the lead of conversation %s: %w", ctx.Run.EntryID, err)
	}
	change, err := e.change(ctx, mode)
	if err != nil {
		return consentChangeFailure(err)
	}
	outcome, err := e.deps.Consent.Record(context.Background(), ctx.Run.WorkspaceID, leadID, change)
	if err != nil {
		return consentChangeFailure(err)
	}
	return routeOutput(&workflow.NodeResult{
		Output: map[string]interface{}{"lead_id": leadID, "changed": outcome.Changed, "event_id": outcome.EventID},
	}, ctx.Graph.OutgoingEdges(ctx.Node.ID), "sucesso"), nil
}

func (e *consentExecutor) change(ctx *workflow.NodeContext, mode string) (lead_domain.ConsentChange, error) {
	change := lead_domain.ConsentChange{
		Kind:   lead_domain.ConsentWithdraw,
		Source: lead_domain.ConsentWorkflow,
		Actor:  lead_domain.ConsentActor{Type: lead_domain.ConsentActorWorkflow, ID: ctx.Run.WorkflowID},
	}
	if mode == workflow.ConsentModeGrant {
		change.Kind, change.Purpose = lead_domain.ConsentGrant, nodeText(ctx, workflow.ConsentPurposeKey)
	}
	messageID := workflow.TriggerMessageID(ctx.State)
	switch {
	case messageID != "":
		evidence, err := e.leadsMessage(ctx, messageID)
		if err != nil {
			return lead_domain.ConsentChange{}, err
		}
		change.Evidence = evidence
		if change.Kind == lead_domain.ConsentWithdraw {
			change.RequestedBy = lead_domain.OptOutLeadRequest
		}
	case change.Kind == lead_domain.ConsentGrant:
		return lead_domain.ConsentChange{}, errConsentGrantWithoutReply
	default:
		change.Evidence = lead_domain.ConsentEvidence{
			Type: lead_domain.EvidenceNote, Ref: consentRunNotePrefix + ctx.Run.ID,
			Excerpt: consentevidence.Excerpt(nodeText(ctx, workflow.ConsentNoteKey)),
		}
	}
	return change, nil
}

func (e *consentExecutor) leadsMessage(ctx *workflow.NodeContext, messageID string) (lead_domain.ConsentEvidence, error) {
	cited, err := e.deps.Messages.GetByID(messageID)
	if err != nil && !errors.Is(err, conversation.ErrMessageNotFound) {
		return lead_domain.ConsentEvidence{}, fmt.Errorf("reading the triggering message %s: %w", messageID, err)
	}
	return consentevidence.LeadMessage(consentevidence.Conversation{EntryID: ctx.Run.EntryID, EntryType: shared.EntryType(ctx.Run.EntryType)}, cited)
}

func (e *checkConsentExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if e.deps.EntryLeads == nil || e.deps.Leads == nil {
		return consentStepFailure("a leitura do consentimento está indisponível neste contexto (ex.: simulação)"), nil
	}
	contact, err := e.leadOfRun(ctx.Run)
	if err != nil {
		return nil, err
	}
	facts := campaign.LeadFactsOf(contact)
	branch := workflow.ConsentBranch(facts.OptedOut, facts.HasConsent)
	next := resolveEdgeByLabelStrict(ctx.Graph.OutgoingEdges(ctx.Node.ID), branch)
	if next == "" {
		return consentStepFailure(fmt.Sprintf("a saída %q do nó não está conectada", branch)), nil
	}
	leadID := ""
	if contact != nil {
		leadID = contact.ID
	}
	return &workflow.NodeResult{NextNodeID: next, Output: map[string]interface{}{"state": branch, "lead_id": leadID}}, nil
}

func (e *checkConsentExecutor) leadOfRun(run *workflow.WorkflowRun) (*lead_domain.Lead, error) {
	leadID, err := e.deps.EntryLeads.LeadOfEntry(context.Background(), run.WorkspaceID, runEntry(run))
	if errors.Is(err, lead_domain.ErrLeadNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the lead of conversation %s: %w", run.EntryID, err)
	}
	contact, err := e.deps.Leads.FindByID(run.WorkspaceID, leadID)
	if errors.Is(err, lead_domain.ErrLeadNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading lead %s: %w", leadID, err)
	}
	return contact, nil
}

func runEntry(run *workflow.WorkflowRun) shared.EntryRef {
	return shared.EntryRef{EntryID: run.EntryID, EntryType: shared.EntryType(run.EntryType)}
}

func consentChangeFailure(err error) (*workflow.NodeResult, error) {
	if lead_domain.IsInputRefusal(err) {
		return codedConsentFailure(err, "o registro foi recusado pelas regras de consentimento"), nil
	}
	return nil, err
}

func codedConsentFailure(err error, reason string) *workflow.NodeResult {
	return consentStepFailure(fmt.Sprintf("%s (%s): %v", reason, lead_domain.ErrorCode(err), err))
}

func consentStepFailure(reason string) *workflow.NodeResult {
	return &workflow.NodeResult{Error: "consentimento não registrado: " + reason}
}
