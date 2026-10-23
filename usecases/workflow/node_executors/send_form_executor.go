package node_executors

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workflow"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

var ErrFormSenderUnavailable = errors.New("workflow: whatsapp forms are not available to this workflow engine")

const (
	formFailureUnknown      = "send_error"
	formFallbackTemplateKey = "fallback_template_id"
	formFallbackParamsKey   = "fallback_params"
)

type formTemplateFallback interface {
	SendTemplate(ctx context.Context, run *workflow.WorkflowRun, templateID, preferredBusinessPhoneID string, paramValues map[string]string, state *workflow.RunState) (*conversation.SendTextMessageOutput, string, error)
}

type FormSender interface {
	SendFromWorkflow(ctx context.Context, in whatsappform_usecase.WorkflowSend) (wf.Send, error)
}

type FormSenderRelay struct {
	mu     sync.RWMutex
	target FormSender
}

func NewFormSenderRelay() *FormSenderRelay {
	return &FormSenderRelay{}
}

func (r *FormSenderRelay) Bind(target FormSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = target
}

func (r *FormSenderRelay) SendFromWorkflow(ctx context.Context, in whatsappform_usecase.WorkflowSend) (wf.Send, error) {
	r.mu.RLock()
	target := r.target
	r.mu.RUnlock()
	if target == nil {
		return wf.Send{}, ErrFormSenderUnavailable
	}
	return target.SendFromWorkflow(ctx, in)
}

type sendFormExecutor struct {
	forms     FormSender
	templates formTemplateFallback
}

func NewSendFormExecutor(forms FormSender, waDeps SenderDeps) workflow.NodeExecutor {
	e := &sendFormExecutor{forms: forms}
	if wa := newWhatsAppSender(waDeps); wa != nil {
		e.templates = wa
	}
	return e
}

func (e *sendFormExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:              workflow.NodeTypeActionSendForm,
		NeedsConversation: true,
		TestBehavior:      workflow.NodeTestWaits,
		Category:          workflow.NodeCategoryAction,
		Scopes:            []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:             "Enviar formulário",
		Description:       "Envia um formulário do WhatsApp e espera o contato responder. Só no WhatsApp oficial, com a janela de 24 horas aberta.",
		Icon:              "ClipboardText",
		Guidance: workflow.NodeGuidance{
			When: "Para coletar vários dados de uma vez (cadastro, pesquisa, agendamento) com um formulário publicado do WhatsApp.",
			Behavior: "O nó PAUSA até o contato enviar o formulário. Respostas comuns de texto NÃO avançam o nó. " +
				"Saídas: 'completed' (formulário enviado pelo contato), 'no_reply' (sem resposta até o timeout), 'send_failed' (não foi possível enviar: janela de 24 horas fechada sem template reserva, canal que não é WhatsApp oficial, formulário não publicado no número). " +
				"Com a janela fechada, o template reserva (um template aprovado com botão deste formulário) é enviado no lugar e o nó continua esperando a resposta. " +
				"Depois de 'completed' o fluxo tem {{form_response_id}} e {{form.<chave_do_campo>}} com o valor exibido de cada resposta (campos sensíveis mascarados).",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: workflow.FormHandleCompleted, Label: "Respondeu"},
			{ID: workflow.FormHandleNoReply, Label: "Não respondeu", Optional: true},
			{ID: workflow.FormHandleSendFailed, Label: "Falha no envio", Optional: true},
		},
		DefaultConfig: map[string]interface{}{
			workflow.FormNodeFormIDKey: "",
			"header":                   "",
			"body":                     "",
			"footer":                   "",
			"cta":                      "",
			"timeout_seconds":          float64(interactiveDefaultTimeoutSeconds),
			formFallbackTemplateKey:    "",
			formFallbackParamsKey:      map[string]interface{}{},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "sent", Description: "true se o formulário foi enviado", Label: "Enviado", Type: workflow.VariableTypeBoolean},
			{Key: workflow.FormNodeFormIDKey, Description: "ID do formulário enviado", Label: "ID do formulário", Type: workflow.VariableTypeText},
			{Key: "send_id", Description: "ID do envio do formulário", Label: "ID do envio", Type: workflow.VariableTypeText},
			{Key: "message_id", Description: "ID da mensagem enviada", Label: "ID da mensagem", Type: workflow.VariableTypeText},
			{Key: "reason", Description: "Motivo quando o envio falha", Label: "Motivo", Type: workflow.VariableTypeText},
			{Key: formFallbackTemplateKey, Description: "ID do template reserva, quando ele foi enviado no lugar do formulário", Label: "ID do template reserva", Type: workflow.VariableTypeText},
			{Key: workflow.DataKeyFormResponseID, Description: "ID da resposta (após a resposta)", Label: "ID da resposta", Type: workflow.VariableTypeText},
			{Key: "form.<campo>", Description: "Resposta de cada campo pela chave do campo (após a resposta)"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: workflow.FormNodeFormIDKey, Label: "Formulário", Type: "select", OptionsSource: "whatsapp_forms", Required: true, Placeholder: "Selecione um formulário publicado"},
			{Key: "header", Label: "Cabeçalho", Type: "text", Placeholder: "Título da mensagem (opcional)"},
			{Key: "body", Label: "Texto da mensagem", Type: "textarea", Placeholder: "Explique o que o contato vai preencher", Required: true},
			{Key: "footer", Label: "Rodapé", Type: "text", Placeholder: "Texto do rodapé (opcional)"},
			{Key: "cta", Label: "Texto do botão", Type: "text", Placeholder: "Abrir formulário", Required: true},
			{Key: "timeout_seconds", Label: "Timeout (segundos)", Type: "number", Placeholder: "300"},
			{Key: formFallbackTemplateKey, Label: "Template reserva", Type: "select", OptionsSource: "whatsapp_templates", Placeholder: "Template aprovado com o botão deste formulário", Description: "Enviado quando a janela de 24 horas está fechada. Opcional."},
			{Key: formFallbackParamsKey, Label: "Parâmetros do template reserva", Type: "keyvalue", Placeholder: "nome=valor (suporta {{var.nome}})"},
		},
	}
}

func (e *sendFormExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	failed := resolveEdgeByLabelStrict(edges, workflow.FormHandleSendFailed)
	if shared.EntryType(ctx.Run.EntryType) != shared.EntryTypeWhatsApp {
		if failed == "" {
			return nil, wf.ErrChannelUnsupported
		}
		skipped := skipUnsupportedNode(ctx, string(workflow.NodeTypeActionSendForm))
		skipped.NextNodeID = failed
		return skipped, nil
	}
	formID := strings.TrimSpace(workflow.Interpolate(stringConfig(ctx.Node.Config, workflow.FormNodeFormIDKey), ctx.State, nil))
	if formID == "" {
		return nil, workflow.ErrNodeConfigMissing
	}

	send, err := e.send(ctx, formID)
	if errors.Is(err, wf.ErrOutsideWindow) {
		if templateID := strings.TrimSpace(workflow.Interpolate(stringConfig(ctx.Node.Config, formFallbackTemplateKey), ctx.State, nil)); templateID != "" {
			return e.sendFallback(ctx, formID, templateID, failed)
		}
	}
	if err != nil {
		return formNotSent(ctx, formID, send.ID, failed, err)
	}
	return parkForForm(ctx, map[string]interface{}{"sent": true, workflow.FormNodeFormIDKey: formID, "send_id": send.ID, "message_id": send.MessageID}), nil
}

func (e *sendFormExecutor) sendFallback(ctx *workflow.NodeContext, formID, templateID, failed string) (*workflow.NodeResult, error) {
	if e.templates == nil {
		return formNotSent(ctx, formID, "", failed, ErrFormSenderUnavailable)
	}
	out, _, err := e.templates.SendTemplate(context.Background(), ctx.Run, templateID, "", templateParamValues(ctx, formFallbackParamsKey), ctx.State)
	if err != nil {
		return formNotSent(ctx, formID, "", failed, err)
	}
	if out == nil || strings.TrimSpace(out.MessageID) == "" {
		return formNotSent(ctx, formID, "", failed, ErrFormSenderUnavailable)
	}
	return parkForForm(ctx, map[string]interface{}{"sent": true, workflow.FormNodeFormIDKey: formID, formFallbackTemplateKey: templateID, "send_id": "", "message_id": out.MessageID}), nil
}

func formNotSent(ctx *workflow.NodeContext, formID, sendID, failed string, err error) (*workflow.NodeResult, error) {
	log.Printf("[workflow][node:%s][run:%s] form %s not sent: %v", ctx.Node.ID, ctx.Run.ID, formID, err)
	if failed == "" {
		return nil, err
	}
	return &workflow.NodeResult{
		NextNodeID: failed,
		Output:     map[string]interface{}{"sent": false, workflow.FormNodeFormIDKey: formID, "send_id": sendID, "message_id": "", "reason": formFailureReason(err)},
	}, nil
}

func parkForForm(ctx *workflow.NodeContext, output map[string]interface{}) *workflow.NodeResult {
	timeout := PromptTimeoutSeconds(ctx.Node.Config)
	return &workflow.NodeResult{
		Output: output,
		Wait: &workflow.WaitInstruction{
			WakeAt: time.Now().UTC().Add(time.Duration(timeout * float64(time.Second))).UnixMilli(),
			Reason: workflow.WaitReasonForm,
		},
	}
}

func (e *sendFormExecutor) send(ctx *workflow.NodeContext, formID string) (wf.Send, error) {
	if e.forms == nil {
		return wf.Send{}, ErrFormSenderUnavailable
	}
	text := func(key string) string {
		return workflow.CustomerText(ctx, stringConfig(ctx.Node.Config, key))
	}
	return e.forms.SendFromWorkflow(context.Background(), whatsappform_usecase.WorkflowSend{
		WorkspaceID: ctx.Run.WorkspaceID,
		EntryID:     ctx.Run.EntryID,
		FormID:      formID,
		RunID:       ctx.Run.ID,
		Copy:        wf.SendCopy{Header: text("header"), Body: text("body"), Footer: text("footer"), CTA: text("cta")},
	})
}

func formFailureReason(err error) string {
	if code := wf.ErrorCode(err); code != "" {
		return code
	}
	return formFailureUnknown
}
