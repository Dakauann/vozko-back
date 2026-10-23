package tools_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/tools"
	"vozko/usecases/transactionalemail"
)

const SendPlatformEmailToolName = "send_platform_email"

type sendPlatformEmailTool struct {
	sender transactionalemail.Sender
}

func NewSendPlatformEmailTool(sender transactionalemail.Sender) tools.Handler {
	return &sendPlatformEmailTool{sender: sender}
}

func (t *sendPlatformEmailTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        SendPlatformEmailToolName,
		DisplayName: "Enviar e-mail (plataforma)",
		Description: "Envia um e-mail transacional pelo e-mail da plataforma, com o remetente padrão do workspace, ao lead desta conversa ou ao endereço em to. " +
			"Passe text (parágrafos separados por uma linha em branco) ou design_id de um design salvo, nunca os dois. " +
			"Assunto e texto aceitam {{first_name}}, {{name}}, {{email}}, {{company}} e {{field.chave}} do lead. Cada envio é cobrado do saldo do workspace; " +
			"o e-mail fica registrado na conversa de e-mail do lead e a resposta traz sent e message_id, ou o código do motivo quando o envio é recusado.",
		DisplayDescription: "Envia e-mails transacionais pelos remetentes cadastrados em E-mail, cobrados do saldo.",
		Parameters: map[string]tools.Parameter{
			"to": {
				Type:               "string",
				Description:        "Opcional: endereço do destinatário. Vazio usa o e-mail do lead da conversa.",
				DisplayName:        "Destinatário",
				DisplayDescription: "Endereço de e-mail do destinatário",
			},
			"subject": {
				Type:               "string",
				Description:        "Assunto do e-mail, em uma linha.",
				DisplayName:        "Assunto",
				DisplayDescription: "Assunto do e-mail",
			},
			"text": {
				Type:               "string",
				Description:        "Corpo em texto simples. Use quando não houver design_id.",
				DisplayName:        "Texto",
				DisplayDescription: "Corpo do e-mail em texto",
			},
			"design_id": {
				Type:               "string",
				Description:        "ID de um design salvo em E-mail. Use quando não houver text.",
				DisplayName:        "Design",
				DisplayDescription: "Design salvo usado como corpo",
			},
		},
		Required:   []string{"subject"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryMessaging,
	}
}

func (t *sendPlatformEmailTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *sendPlatformEmailTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID := configString(config, "__workspace_id")
	if workspaceID == "" {
		return emailToolError("não foi possível identificar o workspace: esta ferramenta só funciona durante um atendimento"), nil
	}
	text, _ := params["text"].(string)
	designID := paramString(params, "design_id")
	if (strings.TrimSpace(text) == "") == (designID == "") {
		return emailToolError("passe text ou design_id, um dos dois"), nil
	}
	if t.sender == nil {
		return platformEmailRefusal(&transactionalemail.Refusal{Code: transactionalemail.CodeUnavailable, Err: transactionalemail.ErrUnavailable}), nil
	}
	req := transactionalemail.Request{
		WorkspaceID: workspaceID,
		To:          paramString(params, "to"),
		Subject:     paramString(params, "subject"),
		Body:        transactionalemail.Body{Text: text, DesignID: designID},
		LeadID:      configString(config, "__lead_id"),
		Origin:      transactionalemail.OriginAgent,
		SentBy:      conversation.SentByAI(agentIDOf(ctx, config)),
	}
	req.IdempotencyKey = agentEmailKey(ctx, configString(config, "__entry_id"), req)
	result, err := t.sender.Send(ctx, req)
	if err != nil {
		return platformEmailRefusal(err), nil
	}
	return tools.ExecutionResult{
		Result: map[string]interface{}{
			"sent":       true,
			"message_id": result.MessageID,
			"to":         result.To,
			"repeated":   result.Repeated,
		},
		ContextUpdateText: fmt.Sprintf("E-mail enviado para %s", result.To),
	}, nil
}

func agentEmailKey(ctx context.Context, entryID string, req transactionalemail.Request) string {
	callID := tools.CallIDFrom(ctx)
	if callID == "" {
		callID = uuid.NewString()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{req.To, req.Subject, req.Body.Text, req.Body.DesignID}, "\x00")))
	return "agent:" + entryID + ":" + callID + ":" + hex.EncodeToString(sum[:8])
}

func platformEmailRefusal(err error) tools.ExecutionResult {
	code := transactionalemail.CodeOf(err)
	result := emailToolError(fmt.Sprintf("e-mail não enviado (%s): %v%s", code, err, transactionalemail.RetryHint(err)))
	result.ErrorCode = code
	return result
}

var _ tools.Handler = (*sendPlatformEmailTool)(nil)
