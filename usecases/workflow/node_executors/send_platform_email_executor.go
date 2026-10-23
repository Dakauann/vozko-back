package node_executors

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/workflow"
	"vozko/usecases/transactionalemail"
)

const (
	PlatformEmailReasonConfigUnreadable = "config_unreadable"
	PlatformEmailReasonLeadUnreadable   = "lead_unreadable"
)

type PlatformEmailNodeDeps struct {
	Sender     transactionalemail.Sender
	EntryLeads lead_domain.EntryLeads
}

type sendPlatformEmailExecutor struct {
	deps PlatformEmailNodeDeps
}

func NewSendPlatformEmailExecutor(deps PlatformEmailNodeDeps) workflow.NodeExecutor {
	return &sendPlatformEmailExecutor{deps: deps}
}

func (e *sendPlatformEmailExecutor) Definition() workflow.NodeDefinition {
	onText := &workflow.FieldRule{Field: workflow.PlatformEmailBodyModeKey, Values: []string{workflow.PlatformEmailBodyText}}
	onDesign := &workflow.FieldRule{Field: workflow.PlatformEmailBodyModeKey, Values: []string{workflow.PlatformEmailBodyDesign}}
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionSendPlatformEmail,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryMessaging,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Enviar e-mail (plataforma)",
		Description:  "Envia um e-mail transacional pelo e-mail da plataforma, com um remetente do seu domínio verificado.",
		Icon:         "EnvelopeSimple",
		Guidance: workflow.NodeGuidance{
			When: "Para mandar um e-mail transacional ao lead (confirmação, código, aviso de pedido) pelos remetentes cadastrados em E-mail.",
			Behavior: "Vai para o e-mail do cadastro do lead, ou para o endereço em 'to'. O corpo é um texto ('body_mode' text) ou um design salvo ('body_mode' design). " +
				"Cobra um envio do saldo antes de enviar e devolve se o envio falhar; respeita a lista de bloqueio e recusa leads bloqueados. Não exige consentimento de marketing e não leva link de descadastro. O e-mail enviado fica registrado na conversa de e-mail do lead, onde a resposta dele também chega. " +
				"Assunto e texto aceitam variáveis do fluxo {{...}} e as do e-mail ({{first_name}}, {{name}}, {{email}}, {{company}}, {{field.chave}}). " +
				"Saídas: 'sent' quando o provedor aceitou e 'failed' quando foi recusado; o motivo fica em reason e error.",
			Examples: []string{
				"config: {\"subject\":\"Pedido {{event.order_id}} confirmado\",\"body_mode\":\"text\",\"text\":\"Oi {{first_name}}, recebemos seu pedido.\"}",
			},
		},
		DefaultConfig: map[string]interface{}{
			workflow.PlatformEmailSenderKey:   "",
			workflow.PlatformEmailToKey:       "",
			workflow.PlatformEmailSubjectKey:  "",
			workflow.PlatformEmailBodyModeKey: workflow.PlatformEmailBodyText,
			workflow.PlatformEmailTextKey:     "",
			workflow.PlatformEmailDesignKey:   "",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: workflow.PlatformEmailHandleSent, Label: "Enviado"},
			{ID: workflow.PlatformEmailHandleFailed, Label: "Falha", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "sent", Description: "true quando o provedor aceitou o e-mail", Label: "Enviado", Type: workflow.VariableTypeBoolean},
			{Key: "message_id", Description: "ID do e-mail na plataforma", Label: "ID da mensagem", Type: workflow.VariableTypeText},
			{Key: "provider_message_id", Description: "ID do e-mail no provedor", Label: "ID no provedor", Type: workflow.VariableTypeText},
			{Key: "to", Description: "Endereço que recebeu o e-mail", Label: "Destinatário", Type: workflow.VariableTypeText},
			{Key: "repeated", Description: "true quando este passo já tinha enviado e o envio não foi repetido", Label: "Envio já feito antes", Type: workflow.VariableTypeBoolean},
			{Key: "reason", Description: "Código do motivo quando o envio falha", Label: "Motivo", Type: workflow.VariableTypeText},
			{Key: "error", Description: "Descrição da recusa quando o envio falha", Label: "Erro", Type: workflow.VariableTypeText},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: workflow.PlatformEmailSenderKey, Label: "Remetente", Type: "select", OptionsSource: "email_senders", Placeholder: "Remetente padrão",
				Description: "Vazio usa o remetente padrão do workspace."},
			{Key: workflow.PlatformEmailToKey, Label: "Destinatário", Type: "text", Placeholder: "E-mail do lead",
				Description: "Vazio usa o e-mail do cadastro do lead. Aceita variáveis do fluxo."},
			{Key: workflow.PlatformEmailSubjectKey, Label: "Assunto", Type: "text", Placeholder: "Pedido confirmado, {{first_name}}", Required: true},
			{Key: workflow.PlatformEmailBodyModeKey, Label: "Conteúdo", Type: "select", Required: true, Options: []workflow.ConfigFieldOption{
				{Value: workflow.PlatformEmailBodyText, Label: "Texto"},
				{Value: workflow.PlatformEmailBodyDesign, Label: "Design salvo"},
			}},
			{Key: workflow.PlatformEmailTextKey, Label: "Texto", Type: "textarea", VisibleWhen: onText, RequiredWhen: onText,
				Description: "Parágrafos separados por uma linha em branco."},
			{Key: workflow.PlatformEmailDesignKey, Label: "Design", Type: "select", OptionsSource: "email_designs", VisibleWhen: onDesign, RequiredWhen: onDesign},
		},
	}
}

func (e *sendPlatformEmailExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if ctx == nil || ctx.Run == nil || ctx.Node == nil {
		return nil, workflow.ErrNodeConfigMissing
	}
	send, err := workflow.ReadPlatformEmailSend(ctx.Node.Config)
	if err != nil {
		return platformEmailFailed(ctx, PlatformEmailReasonConfigUnreadable, err), nil
	}
	if e.deps.Sender == nil {
		return platformEmailFailed(ctx, transactionalemail.CodeUnavailable, transactionalemail.ErrUnavailable), nil
	}
	leadID, err := e.leadOf(ctx.Run)
	if err != nil {
		return platformEmailFailed(ctx, PlatformEmailReasonLeadUnreadable, err), nil
	}
	rendered := send.Render(customerTextIn(ctx))
	req := transactionalemail.Request{
		WorkspaceID:    ctx.Run.WorkspaceID,
		SenderID:       send.SenderID,
		To:             strings.TrimSpace(rendered.To),
		Subject:        rendered.Subject,
		LeadID:         leadID,
		IdempotencyKey: "workflow:" + ctx.Run.ID + ":" + ctx.Node.ID + ":" + ctx.Run.NodeVisit(),
		Origin:         transactionalemail.OriginWorkflow,
		SentBy:         conversation.SentByWorkflow(ctx.Run.WorkflowID),
	}
	if send.Mode == workflow.PlatformEmailBodyDesign {
		req.Body.DesignID = send.DesignID
	} else {
		req.Body.Text = rendered.Text
	}

	callCtx, release := ctx.CallContext()
	defer release()
	result, err := e.deps.Sender.Send(callCtx, req)
	if err != nil && transactionalemail.Retryable(err) && canRetryNode(ctx) {
		log.Printf("[workflow][node:%s][run:%s] send_platform_email will retry with the same key: %v", ctx.Node.ID, ctx.Run.ID, err)
		return nil, err
	}
	if err != nil {
		log.Printf("[workflow][node:%s][run:%s] send_platform_email refused: %v", ctx.Node.ID, ctx.Run.ID, err)
		failed := platformEmailFailed(ctx, transactionalemail.CodeOf(err), err)
		if seconds, ok := transactionalemail.RetryAfterSeconds(err); ok {
			failed.Output["retry_after_seconds"] = seconds
		}
		return failed, nil
	}
	output := map[string]interface{}{
		"sent":                true,
		"message_id":          result.MessageID,
		"provider_message_id": result.ProviderMessageID,
		"to":                  result.To,
		"repeated":            result.Repeated,
	}
	return routeOutput(&workflow.NodeResult{Output: output}, outgoingEdges(ctx), workflow.PlatformEmailHandleSent), nil
}

func (e *sendPlatformEmailExecutor) leadOf(run *workflow.WorkflowRun) (string, error) {
	if run.OnLead() {
		return run.EntryID, nil
	}
	if e.deps.EntryLeads == nil {
		return "", nil
	}
	leadID, err := e.deps.EntryLeads.LeadOfEntry(context.Background(), run.WorkspaceID, runEntry(run))
	if err != nil && !errors.Is(err, lead_domain.ErrLeadNotFound) {
		return "", err
	}
	return strings.TrimSpace(leadID), nil
}

func canRetryNode(ctx *workflow.NodeContext) bool {
	return ctx.Run.RetryCount < workflow.MaxRetriesPerNode && !workflow.InCall(ctx.Runtime)
}

func platformEmailFailed(ctx *workflow.NodeContext, reason string, cause error) *workflow.NodeResult {
	output := map[string]interface{}{
		"sent":   false,
		"reason": reason,
		"error":  cause.Error(),
	}
	return routeOutput(&workflow.NodeResult{Output: output}, outgoingEdges(ctx), workflow.PlatformEmailHandleFailed)
}
