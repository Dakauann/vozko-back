package tools_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/brand"
	"vozko/domain/tools"
	email_usecase "vozko/usecases/email"
)

const SendEmailToolName = "send_email"

type sendEmailTool struct {
	sender email_usecase.SMTPSender
}

func NewSendEmailToolUseCase(sender email_usecase.SMTPSender) tools.Handler {
	if sender == nil {
		sender = email_usecase.NewGoMailSMTPSender()
	}
	return &sendEmailTool{sender: sender}
}

func (uc *sendEmailTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               SendEmailToolName,
		DisplayName:        "Enviar E-mail",
		Description:        "Envia um e-mail usando a configuração SMTP própria do usuário.",
		DisplayDescription: "Envia e-mails pelo servidor SMTP configurado pelo usuário, sem usar o e-mail da " + brand.Active().Name + ".",
		Parameters: map[string]tools.Parameter{
			"to": {
				Type:               "string",
				Description:        "Endereço de e-mail do destinatário. Aceita múltiplos endereços separados por vírgula.",
				DisplayName:        "Destinatário",
				DisplayDescription: "Endereço de e-mail do destinatário",
			},
			"cc": {
				Type:               "string",
				Description:        "Destinatários em cópia, separados por vírgula",
				DisplayName:        "Cc",
				DisplayDescription: "Destinatários em cópia",
			},
			"bcc": {
				Type:               "string",
				Description:        "Destinatários em cópia oculta, separados por vírgula",
				DisplayName:        "Bcc",
				DisplayDescription: "Destinatários em cópia oculta",
			},
			"subject": {
				Type:               "string",
				Description:        "Assunto do e-mail",
				DisplayName:        "Assunto",
				DisplayDescription: "Assunto do e-mail",
			},
			"body": {
				Type:               "string",
				Description:        "Conteúdo (HTML ou texto) do e-mail",
				DisplayName:        "Conteúdo",
				DisplayDescription: "Conteúdo do e-mail (HTML ou texto)",
			},
			"body_type": {
				Type:               "string",
				Description:        "Formato do conteúdo: html ou text",
				DisplayName:        "Formato do conteúdo",
				DisplayDescription: "Formato HTML ou texto simples",
				Enum:               []string{"html", "text"},
			},
			"reply_to": {
				Type:               "string",
				Description:        "Endereço de resposta opcional",
				DisplayName:        "Responder para",
				DisplayDescription: "Endereço usado no cabeçalho Reply-To",
			},
		},
		Required: []string{"to", "subject", "body"},
		ConfigSchema: map[string]tools.ConfigParameter{
			"smtp_host": {
				Type:               "string",
				Description:        "Hostname do servidor SMTP, sem protocolo ou porta",
				DisplayName:        "Servidor SMTP",
				DisplayDescription: "Hostname do servidor SMTP",
				Required:           true,
			},
			"smtp_port": {
				Type:               "number",
				Description:        "Porta SMTP. Use 587 para STARTTLS, 465 para TLS implícito ou 25 para relay sem TLS.",
				DisplayName:        "Porta SMTP",
				DisplayDescription: "Porta do servidor SMTP",
				Default:            587,
			},
			"smtp_security": {
				Type:               "string",
				Description:        "Modo de segurança SMTP",
				DisplayName:        "Segurança SMTP",
				DisplayDescription: "STARTTLS, TLS implícito ou sem TLS para relays sem autenticação",
				Default:            string(email_usecase.SMTPSecurityStartTLS),
				Options: []tools.ConfigParameterOption{
					{Value: string(email_usecase.SMTPSecurityStartTLS), Label: "STARTTLS"},
					{Value: string(email_usecase.SMTPSecurityImplicitTLS), Label: "TLS implícito"},
					{Value: string(email_usecase.SMTPSecurityNone), Label: "Sem TLS"},
				},
			},
			"smtp_username": {
				Type:               "string",
				Description:        "Usuário SMTP opcional",
				DisplayName:        "Usuário SMTP",
				DisplayDescription: "Usuário para autenticação SMTP",
			},
			"smtp_password": {
				Type:               "string",
				Description:        "Senha ou app password SMTP opcional",
				DisplayName:        "Senha SMTP",
				DisplayDescription: "Senha ou app password SMTP",
			},
			"smtp_from_email": {
				Type:               "string",
				Description:        "Endereço usado no cabeçalho From e no envelope SMTP",
				DisplayName:        "E-mail remetente",
				DisplayDescription: "Endereço remetente autorizado pelo servidor SMTP",
				Required:           true,
			},
			"smtp_from_name": {
				Type:               "string",
				Description:        "Nome exibido para o remetente",
				DisplayName:        "Nome do remetente",
				DisplayDescription: "Nome exibido no campo From",
			},
			"default_reply_to": {
				Type:               "string",
				Description:        "Endereço Reply-To padrão opcional",
				DisplayName:        "Responder para padrão",
				DisplayDescription: "Endereço Reply-To usado quando a chamada não informar outro",
			},
			"timeout_seconds": {
				Type:               "number",
				Description:        "Tempo máximo para conectar e enviar o e-mail",
				DisplayName:        "Timeout",
				DisplayDescription: "Timeout em segundos",
				Default:            30,
			},
		},
		RequiredConfig: []string{"smtp_host", "smtp_from_email"},
		RequiresConfig: true,
		Visibility:     []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:       tools.CategoryMessaging,
	}
}

func (uc *sendEmailTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return emailToolError("configuração SMTP do usuário é obrigatória para enviar e-mail"), nil
}

func (uc *sendEmailTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	if uc == nil || uc.sender == nil {
		return emailToolError("serviço de SMTP indisponível"), nil
	}
	cfg, err := email_usecase.SMTPConfigFromMap(config)
	if err != nil {
		return emailToolError(fmt.Sprintf("configuração SMTP inválida: %v", err)), nil
	}
	message, err := email_usecase.EmailMessageFromMap(params)
	if err != nil {
		return emailToolError(fmt.Sprintf("parâmetros de e-mail inválidos: %v", err)), nil
	}
	result, err := uc.sender.Send(ctx, cfg, message)
	if err != nil {
		return emailToolError(fmt.Sprintf("erro ao enviar e-mail: %v", err)), nil
	}

	to := strings.Join(result.To, ", ")
	return tools.ExecutionResult{
		Result: map[string]interface{}{
			"sent":            true,
			"to":              result.To,
			"cc":              result.Cc,
			"bcc":             result.Bcc,
			"subject":         result.Subject,
			"message_id":      result.MessageID,
			"server_response": result.ServerResponse,
		},
		ContextUpdateText: fmt.Sprintf("E-mail enviado para %s", to),
	}, nil
}

func emailToolError(message string) tools.ExecutionResult {
	return tools.ExecutionResult{Result: message, IsError: true}
}

var _ tools.Handler = (*sendEmailTool)(nil)
