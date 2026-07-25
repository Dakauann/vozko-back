package node_executors

import (
	"log"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type sendTextExecutor struct {
	sender *whatsappSender
}

func NewSendTextExecutor(waDeps WhatsAppSenderDeps) workflow.NodeExecutor {
	return &sendTextExecutor{sender: newWhatsAppSender(waDeps)}
}

func (e *sendTextExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionSendText,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:       "Enviar Texto",
		Description: "Envia uma mensagem de texto ao contato.",
		Icon:        "PaperPlaneTilt",
		Guidance: workflow.NodeGuidance{
			When: "Para enviar uma mensagem de texto ao contato (texto fixo ou montado com variáveis). Útil para enviar a saída de um nó anterior, ou em canais onde o agente não envia sozinho.",
			Examples: []string{
				"config: {\"text\":\"Seu endereço: {{node.n4.logradouro}}, {{node.n4.bairro}} - {{node.n4.localidade}}/{{node.n4.uf}}\"}",
			},
		},
		DefaultConfig: map[string]interface{}{
			"text": "",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "text", Description: "Texto enviado"},
			{Key: "sent", Description: "true se enviado com sucesso"},
			{Key: "message_id", Description: "ID da mensagem (WhatsApp)"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "text", Label: "Texto da Mensagem", Type: "textarea", Placeholder: "Digite o texto...", Required: true},
		},
	}
}

func (e *sendTextExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	text, _ := ctx.Node.Config["text"].(string)
	if text == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	text = workflow.Interpolate(text, ctx.State, nil)
	// TODO: validate if right behavior, pretty sure this will need changes if we implement workflows for voip calls
	if e.sender != nil && shared.EntryType(ctx.Run.EntryType) == shared.EntryTypeWhatsApp {
		sendOutput, usedBusinessPhoneID, err := e.sender.SendText(nil, ctx.Run, text, conversation.MessageTypeAIResponse)
		if err != nil {
			return nil, err
		}
		if sendOutput != nil {
			log.Printf("[workflow][node:%s][run:%s] action_send_text: sent WhatsApp text to entry=%s via phone=%s wamid=%s",
				ctx.Node.ID, ctx.Run.ID, ctx.Run.EntryID, usedBusinessPhoneID, sendOutput.MessageID)
			return &workflow.NodeResult{
				Output: map[string]interface{}{
					"text":              text,
					"sent":              true,
					"message_id":        sendOutput.MessageID,
					"contact_wa_id":     sendOutput.ContactWaID,
					"business_phone_id": usedBusinessPhoneID,
				},
			}, nil
		}
	}
	log.Printf("[workflow][node:%s][run:%s] action_send_text: to entry=%s, text=%q (stub — real send not wired yet)",
		ctx.Node.ID, ctx.Run.ID, ctx.Run.EntryID, text)
	return &workflow.NodeResult{
		Output: map[string]interface{}{"text": text, "sent": true},
	}, nil
}
