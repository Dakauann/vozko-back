package tools_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/tools"
)

// ToolNameSendOptions is channel-neutral on purpose. The name reaches the model
// verbatim, and a tool called "send_whatsapp_button_message" offered inside a
// Telegram conversation reads as belonging elsewhere. Saved bindings under the
// old name still resolve through CanonicalToolName.
const ToolNameSendOptions = "send_options"

type sendWhatsappButtonMessageTool struct {
	ctx                   context.Context
	whatsappClientFactory conversation.WhatsAppClientFactory
	// adapters routes the prompt on every channel that is not WhatsApp.
	// Optional: unset keeps the tool WhatsApp-only, as it was.
	adapters conversation.AdapterRegistry
}

// SetAdapters wires the channel registry so the tool can present options
// anywhere the channel supports them.
func (uc *sendWhatsappButtonMessageTool) SetAdapters(r conversation.AdapterRegistry) {
	uc.adapters = r
}

func NewSendWhatsappButtonMessageToolUseCase(ctx context.Context, whatsappClientFactory conversation.WhatsAppClientFactory) tools.Handler {
	return &sendWhatsappButtonMessageTool{
		whatsappClientFactory: whatsappClientFactory,
		ctx:                   ctx,
	}
}

func (uc *sendWhatsappButtonMessageTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               ToolNameSendOptions,
		DisplayName:        "Enviar Opções",
		DisplayDescription: "Envia uma mensagem com botões de resposta rápida ou botão de cópia (Pix, cupons, etc.).",
		Description: `Envia uma mensagem com botões interativos para o cliente atual.

O destinatário é obtido automaticamente do contexto da conversa - NÃO é necessário informar.

Funciona em todos os canais que suportam escolhas: WhatsApp (botões ou lista),
Instagram (respostas rápidas) e Telegram (teclado inline). Cada canal renderiza
no formato nativo dele e aplica os próprios limites; opções além do limite de um
canal simplesmente não aparecem nele. O botão de cópia (copy_code) existe apenas
no WhatsApp.

DOIS MODOS DE USO:

1. BOTÕES DE RESPOSTA RÁPIDA (type="reply"), máximo 3 botões:
   Use quando o usuário precisa escolher entre opções:
   - Perguntar sexo → "Homem" / "Mulher"
   - Confirmação → "Sim" / "Não"
   - Escolher plano/produto → "Básico" / "Intermediário" / "Premium"
   - Forma de pagamento → "Pix" / "boleto", ou qualquer outro
   - Consentimento LGPD → "SIM, aceito" / "NÃO aceito"
   - Qualquer seleção de confirmação, ou demonstração de opções
   O usuário clica no botão e você recebe a resposta automaticamente.

2. BOTÃO DE CÓPIA (type="copy_code"), exatamente 1 botão:
   Use quando o usuário precisa copiar um valor para a área de transferência:
   - Enviar chave Pix → o usuário toca para copiar a chave
   - Enviar código de cupom/desconto
   - Enviar código de acesso / token / senha temporária
   - Enviar link longo / boleto / código de barras
   O botão aparece com um ícone de cópia; ao toque, copia o valor automaticamente.
   NÃO pode ser misturado com botões reply.

DICA: Após usar esta ferramenta, evite repetir o conteúdo dos botões em texto.`,
		Parameters: map[string]tools.Parameter{
			"body": {
				Type:               "string",
				Description:        "O texto da pergunta ou mensagem principal que será exibida acima dos botões",
				DisplayName:        "Mensagem",
				DisplayDescription: "Texto da mensagem exibida acima dos botões",
			},
			"buttons": {
				Type: "array",
				Description: `Lista de botões. Suporta dois tipos:

` +
					`TIPO 1, Resposta rápida ("type": "reply"):
` +
					`  Usado quando o usuário precisa escolher entre opções. Mínimo 1, máximo 3.
` +
					`  Ex: confirmar agendamento, escolher plano, aceitar LGPD.
` +
					`  Campos: type="reply", id (interno), title (texto no botão, máx 20 chars).

` +
					`TIPO 2, Copiar valor ("type": "copy_code"):
` +
					`  Usado quando o usuário precisa copiar um dado para a área de transferência.
` +
					`  USE SOMENTE quando enviar: chave Pix, código de cupom, código de acesso,
` +
					`  link de boleto, senha temporária ou qualquer texto que o usuário deva copiar.
` +
					`  Apenas 1 botão copy_code por mensagem. NÃO pode ser misturado com reply.
` +
					`  Campos: type="copy_code", value (o texto que será copiado).`,
				DisplayName:        "Botões",
				DisplayDescription: "Lista de botões de resposta rápida ou cópia (máx. 3)",
				Items: &tools.ParameterItems{
					Type:        "object",
					Description: "Um botão (reply ou copy_code)",
					Properties: map[string]tools.Parameter{
						"type": {
							Type:               "string",
							Description:        `Tipo do botão: "reply" para resposta rápida, "copy_code" para botão de cópia.`,
							DisplayName:        "Tipo",
							DisplayDescription: "Tipo do botão",
							Enum:               []string{"reply", "copy_code"},
						},
						"id": {
							Type:               "string",
							Description:        "(Somente reply) Identificador único interno do botão (ex: 'male', 'opt1')",
							DisplayName:        "Identificador",
							DisplayDescription: "ID interno do botão (somente reply)",
						},
						"title": {
							Type:               "string",
							Description:        "(Somente reply) Texto exibido no botão (máximo 20 caracteres)",
							DisplayName:        "Título",
							DisplayDescription: "Texto exibido no botão (máx. 20 caracteres, somente reply)",
						},
						"value": {
							Type:               "string",
							Description:        "(Somente copy_code) O valor exato que será copiado para a área de transferência do usuário (ex: chave Pix, código de cupom)",
							DisplayName:        "Valor a copiar",
							DisplayDescription: "Valor copiado ao clicar (somente copy_code)",
						},
					},
					Required: []string{"type"},
				},
			},
			"header": {
				Type:               "string",
				Description:        "(Opcional) Texto de cabeçalho exibido acima da mensagem principal",
				DisplayName:        "Cabeçalho",
				DisplayDescription: "Texto do cabeçalho (opcional)",
			},
			"footer": {
				Type:               "string",
				Description:        "(Opcional) Texto de rodapé exibido abaixo dos botões",
				DisplayName:        "Rodapé",
				DisplayDescription: "Texto do rodapé (opcional)",
			},
		},
		Required:   []string{"body", "buttons"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryMessaging,
	}
}

func (uc *sendWhatsappButtonMessageTool) resolveRecipientPhone(ctx context.Context, config map[string]interface{}, data map[string]interface{}) (string, error) {
	if config != nil {
		if phone, ok := config["__recipient_phone"].(string); ok && phone != "" {
			log.Printf("[WhatsApp Button Tool] Using phone from config: %s", phone)
			return phone, nil
		}
	}

	if to, ok := data["to"].(string); ok && to != "" {
		log.Printf("[WhatsApp Button Tool] WARNING: Using AI-provided phone (may be incorrect): %s", to)
		return strings.TrimSpace(to), nil
	}

	return "", fmt.Errorf("no phone number available - must be in messaging context or voice call")
}

func (uc *sendWhatsappButtonMessageTool) resolveWhatsAppClient(config map[string]interface{}) (conversation.WhatsAppClient, error) {
	if config != nil {
		if phoneID, ok := config["__business_phone_id"].(string); ok && phoneID != "" {
			return uc.whatsappClientFactory.ClientForPhone(phoneID)
		}
	}
	return nil, fmt.Errorf("no WhatsApp client available: missing __business_phone_id in config")
}

func (uc *sendWhatsappButtonMessageTool) Execute(ctx context.Context, data map[string]interface{}) (tools.ExecutionResult, error) {
	return uc.executeWithPhone(ctx, nil, data)
}

func (uc *sendWhatsappButtonMessageTool) executeWithPhone(ctx context.Context, config map[string]interface{}, data map[string]interface{}) (tools.ExecutionResult, error) {
	// Non-WhatsApp channels are addressed by conversation and resolved before a
	// phone number is looked for, there is none to find.
	adapter, ec, viaAdapter := resolveToolAdapter(ctx, uc.adapters, config)

	var whatsappClient conversation.WhatsAppClient
	var to string
	if !viaAdapter {
		var err error
		if whatsappClient, err = uc.resolveWhatsAppClient(config); err != nil {
			return tools.ExecutionResult{}, err
		}
		if to, err = uc.resolveRecipientPhone(ctx, config, data); err != nil {
			return tools.ExecutionResult{}, err
		}
	}

	body, ok := data["body"].(string)
	if !ok {
		return tools.ExecutionResult{}, fmt.Errorf("missing or invalid parameter 'body'")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return tools.ExecutionResult{}, fmt.Errorf("parameter 'body' is required")
	}

	buttonsRaw, ok := data["buttons"]
	if !ok {
		return tools.ExecutionResult{}, fmt.Errorf("missing parameter 'buttons'")
	}

	buttonsArray, ok := buttonsRaw.([]interface{})
	if !ok {
		return tools.ExecutionResult{}, fmt.Errorf("parameter 'buttons' must be an array")
	}

	if len(buttonsArray) == 0 {
		return tools.ExecutionResult{}, fmt.Errorf("at least one button is required")
	}

	if len(buttonsArray) > 3 {
		return tools.ExecutionResult{}, fmt.Errorf("maximum 3 buttons allowed")
	}

	var buttons []conversation.InteractiveButton
	for i, btnRaw := range buttonsArray {
		btnMap, ok := btnRaw.(map[string]interface{})
		if !ok {
			return tools.ExecutionResult{}, fmt.Errorf("button at index %d must be an object", i)
		}

		btnType := strings.ToLower(strings.TrimSpace(func() string {
			v, _ := btnMap["type"].(string)
			return v
		}()))
		if btnType == "" {
			btnType = "reply"
		}

		switch btnType {
		case "copy_code", "copy":
			value, _ := btnMap["value"].(string)
			value = strings.TrimSpace(value)
			if value == "" {
				return tools.ExecutionResult{}, fmt.Errorf("copy_code button at index %d is missing 'value'", i)
			}
			buttons = append(buttons, conversation.InteractiveButton{
				Type:     conversation.ButtonTypeCopyCode,
				CopyCode: value,
			})
		default:
			id, _ := btnMap["id"].(string)
			title, _ := btnMap["title"].(string)
			id = strings.TrimSpace(id)
			title = strings.TrimSpace(title)
			if id == "" {
				id = fmt.Sprintf("btn_%d", i+1)
			}
			if title == "" {
				return tools.ExecutionResult{}, fmt.Errorf("reply button at index %d is missing 'title'", i)
			}
			if len(title) > 20 {
				title = title[:20]
			}
			buttons = append(buttons, conversation.InteractiveButton{
				Type:  conversation.ButtonTypeReply,
				ID:    id,
				Title: title,
			})
		}
	}

	header := ""
	if headerRaw, ok := data["header"].(string); ok {
		header = strings.TrimSpace(headerRaw)
	}

	footer := ""
	if footerRaw, ok := data["footer"].(string); ok {
		footer = strings.TrimSpace(footerRaw)
	}

	if viaAdapter {
		options := make([]conversation.InteractiveOption, 0, len(buttons))
		for _, b := range buttons {
			// Only reply buttons cross channels. copy_code is a WhatsApp
			// affordance with no equivalent elsewhere, and silently turning one
			// into a plain button would hand the contact something that looks
			// tappable and copies nothing.
			if b.Type != "" && b.Type != "reply" {
				continue
			}
			options = append(options, conversation.InteractiveOption{ID: b.ID, Title: b.Title})
		}
		if len(options) == 0 {
			return toolRefusal(fmt.Sprintf(
				"O canal %s não suporta este tipo de botão. Envie a informação em texto.", ec.EntryType)), nil
		}
		return sendOptionsViaAdapter(ctx, adapter, ec, conversation.SendInteractiveRequest{
			Body:    body,
			Header:  header,
			Footer:  footer,
			Options: options,
			Style:   channel.InteractiveStyleButtons,
		})
	}

	input := conversation.SendButtonMessageInput{
		To:         to,
		BodyText:   body,
		Buttons:    buttons,
		FooterText: footer,
	}

	if header != "" {
		input.HeaderType = conversation.HeaderTypeText
		input.HeaderText = header
	}

	activeCtx := uc.ctx
	if activeCtx == nil {
		activeCtx = ctx
	}
	if activeCtx == nil {
		activeCtx = context.Background()
	}

	log.Printf("[Options Tool] Sending %d option(s) to: %s", len(buttons), to)
	result, err := whatsappClient.SendButtonMessage(activeCtx, input)
	if err != nil {
		log.Printf("[WhatsApp Button Tool] ERROR: %v", err)
		return tools.ExecutionResult{Result: result}, err
	}
	log.Printf("[WhatsApp Button Tool] SUCCESS: MessageID=%s, Status=%s", result.MessageID, result.MessageStatus)

	return tools.ExecutionResult{
		Result: map[string]interface{}{
			"success":    true,
			"message_id": result.MessageID,
			"status":     result.MessageStatus,
		},
	}, nil
}

func (uc *sendWhatsappButtonMessageTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	return uc.executeWithPhone(ctx, config, params)
}

var _ tools.Handler = (*sendWhatsappButtonMessageTool)(nil)
