package node_executors

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"

	"vozko/domain/workflow"
	email_usecase "vozko/usecases/email"
)

type sendEmailExecutor struct {
	sender email_usecase.SMTPSender
}

func NewSendEmailExecutor(sender email_usecase.SMTPSender) workflow.NodeExecutor {
	return &sendEmailExecutor{sender: sender}
}

func (e *sendEmailExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionSendEmail,
		Category:    workflow.NodeCategoryMessaging,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Enviar E-mail",
		Description: "Envia um e-mail usando um servidor SMTP configurado pelo usuário.",
		Icon:        "EnvelopeSimple",
		Guidance: workflow.NodeGuidance{
			When: "Para enviar e-mail via SMTP.",
		},
		DefaultConfig: map[string]interface{}{
			"smtp_host":       "",
			"smtp_port":       587,
			"smtp_security":   string(email_usecase.SMTPSecurityStartTLS),
			"smtp_username":   "",
			"smtp_password":   "",
			"smtp_from_email": "",
			"smtp_from_name":  "",
			"to":              "{{email}}",
			"subject":         "",
			"body":            "",
			"body_type":       string(email_usecase.EmailBodyTypeHTML),
			"timeout_seconds": 30,
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "falha", Label: "Falha", Optional: true},
			{ID: "timeout", Label: "Tempo esgotado", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "to", Description: "Destinatários principais usados no envio"},
			{Key: "cc", Description: "Destinatários em cópia"},
			{Key: "bcc", Description: "Destinatários em cópia oculta"},
			{Key: "subject", Description: "Assunto enviado"},
			{Key: "message_id", Description: "Message-ID gerado para o e-mail"},
			{Key: "server_response", Description: "Resposta do servidor SMTP, quando disponível"},
			{Key: "sent", Description: "true se o e-mail foi enviado com sucesso"},
			{Key: "success", Description: "Alias booleano para sucesso"},
			{Key: "error", Description: "Erro quando o envio não foi realizado"},
			{Key: "next_edge", Description: "Saída seguida pelo workflow: sucesso, falha ou timeout"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "smtp_host", Label: "Servidor SMTP", Type: "text", Placeholder: "smtp.example.com", Required: true, Description: "Hostname do servidor SMTP, sem protocolo ou porta."},
			{Key: "smtp_port", Label: "Porta SMTP", Type: "number", Placeholder: "587", Description: "587 para STARTTLS, 465 para TLS implícito ou 25 para relay sem TLS."},
			{Key: "smtp_security", Label: "Segurança SMTP", Type: "select", Required: true, Options: []workflow.ConfigFieldOption{
				{Value: string(email_usecase.SMTPSecurityStartTLS), Label: "STARTTLS"},
				{Value: string(email_usecase.SMTPSecurityImplicitTLS), Label: "TLS implícito"},
				{Value: string(email_usecase.SMTPSecurityNone), Label: "Sem TLS"},
			}},
			{Key: "smtp_username", Label: "Usuário SMTP", Type: "text", Placeholder: "usuario@example.com", Description: "Opcional para relays sem autenticação."},
			{Key: "smtp_password", Label: "Senha SMTP", Type: "text", Placeholder: "senha ou app password", Description: "Obrigatória quando usuário SMTP for informado."},
			{Key: "smtp_from_email", Label: "E-mail remetente", Type: "text", Placeholder: "usuario@example.com", Required: true, Description: "Endereço autorizado pelo servidor SMTP."},
			{Key: "smtp_from_name", Label: "Nome do remetente", Type: "text", Placeholder: "Nome da empresa"},
			{Key: "default_reply_to", Label: "Responder para padrão", Type: "text", Placeholder: "resposta@example.com", Description: "Reply-To padrão opcional."},
			{Key: "to", Label: "Destinatário", Type: "text", Placeholder: "{{email}} ou cliente@example.com", Required: true, Description: "Aceita variáveis e múltiplos endereços separados por vírgula."},
			{Key: "cc", Label: "Cc", Type: "text", Placeholder: "copia@example.com", Description: "Destinatários em cópia, separados por vírgula."},
			{Key: "bcc", Label: "Bcc", Type: "text", Placeholder: "oculto@example.com", Description: "Destinatários em cópia oculta, separados por vírgula."},
			{Key: "reply_to", Label: "Responder para", Type: "text", Placeholder: "resposta@example.com", Description: "Reply-To específico deste envio."},
			{Key: "subject", Label: "Assunto", Type: "text", Placeholder: "Olá {{name}}", Required: true, Description: "Aceita variáveis do workflow."},
			{Key: "body", Label: "Conteúdo", Type: "textarea", Placeholder: "<p>Olá {{name}}</p>", Required: true, Description: "Conteúdo HTML ou texto simples. Aceita variáveis do workflow."},
			{Key: "body_type", Label: "Formato", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: string(email_usecase.EmailBodyTypeHTML), Label: "HTML"},
				{Value: string(email_usecase.EmailBodyTypeText), Label: "Texto"},
			}},
			{Key: "timeout_seconds", Label: "Timeout", Type: "number", Placeholder: "30", Description: "Tempo máximo para conectar e enviar."},
		},
	}
}

func (e *sendEmailExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if ctx == nil || ctx.Run == nil || ctx.Node == nil {
		return nil, workflow.ErrNodeConfigMissing
	}
	if e.sender == nil {
		return nil, fmt.Errorf("email sender is not configured")
	}

	config := interpolateEmailNodeConfig(ctx.Node.Config, ctx.State)
	cfg, err := email_usecase.SMTPConfigFromMap(config)
	if err != nil {
		return emailFailureResult(ctx, nil, "falha", fmt.Errorf("configuração SMTP inválida: %w", err)), nil
	}
	message, err := email_usecase.EmailMessageFromMap(config)
	if err != nil {
		return emailFailureResult(ctx, nil, "falha", fmt.Errorf("parâmetros de e-mail inválidos: %w", err)), nil
	}

	result, err := e.sender.Send(context.Background(), cfg, message)
	if err != nil {
		label := "falha"
		if isSMTPTimeout(err) {
			label = "timeout"
		}
		log.Printf("[workflow][node:%s][run:%s] send_email error: %v", ctx.Node.ID, ctx.Run.ID, err)
		return emailFailureResult(ctx, &message, label, err), nil
	}

	edges := outgoingEmailEdges(ctx)
	nextID := resolveEdgeByLabel(edges, "sucesso")
	output := map[string]interface{}{
		"to":              result.To,
		"cc":              result.Cc,
		"bcc":             result.Bcc,
		"subject":         result.Subject,
		"message_id":      result.MessageID,
		"server_response": result.ServerResponse,
		"sent":            true,
		"success":         true,
		"next_edge":       "sucesso",
		"next_node_id":    nextID,
	}
	return &workflow.NodeResult{NextNodeID: nextID, Output: output}, nil
}

func emailFailureResult(ctx *workflow.NodeContext, message *email_usecase.EmailMessage, label string, err error) *workflow.NodeResult {
	edges := outgoingEmailEdges(ctx)
	nextID := resolveEdgeByLabel(edges, label)
	output := map[string]interface{}{
		"sent":         false,
		"success":      false,
		"error":        err.Error(),
		"next_edge":    label,
		"next_node_id": nextID,
	}
	if message != nil {
		output["to"] = message.To
		output["cc"] = message.Cc
		output["bcc"] = message.Bcc
		output["subject"] = message.Subject
	}
	return &workflow.NodeResult{NextNodeID: nextID, Output: output}
}

func interpolateEmailNodeConfig(config map[string]interface{}, state *workflow.RunState) map[string]interface{} {
	if config == nil {
		return map[string]interface{}{}
	}
	if state == nil {
		emptyState := workflow.NewRunState()
		state = &emptyState
	}
	result := make(map[string]interface{}, len(config))
	for key, value := range config {
		result[key] = interpolateEmailNodeValue(value, state)
	}
	return result
}

func interpolateEmailNodeValue(value interface{}, state *workflow.RunState) interface{} {
	switch typed := value.(type) {
	case string:
		return workflow.Interpolate(typed, state, nil)
	case []interface{}:
		items := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			items = append(items, interpolateEmailNodeValue(item, state))
		}
		return items
	case []string:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, workflow.Interpolate(item, state, nil))
		}
		return items
	default:
		return typed
	}
}

func outgoingEmailEdges(ctx *workflow.NodeContext) []workflow.Edge {
	if ctx == nil || ctx.Graph == nil || ctx.Node == nil {
		return nil
	}
	return ctx.Graph.OutgoingEdges(ctx.Node.ID)
}

func isSMTPTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
