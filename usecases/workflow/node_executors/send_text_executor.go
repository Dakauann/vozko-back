package node_executors

import (
	"log"
	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

type sendTextExecutor struct {
	sender *channelSender
}

func NewSendTextExecutor(deps SenderDeps) workflow.NodeExecutor {
	return &sendTextExecutor{sender: newChannelSender(deps)}
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
			{Key: "message_id", Description: "ID da mensagem no canal"},
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

	// Text is the one message shape every channel has, so this node routes
	// through the channel sender rather than testing the entry type: WhatsApp
	// keeps its dedicated path, and any adapter-backed channel (Instagram today,
	// Telegram next) sends with no change here.
	if !e.sender.Supports(ctx.Run) {
		return skipUnsupportedNode(ctx, "action_send_text"), nil
	}

	sent, err := e.sender.SendText(nil, ctx.Run, text, conversation.MessageTypeAIResponse)
	if err != nil {
		return nil, err
	}
	if sent == nil {
		// The channel declined without failing, a closed outbound window is the
		// usual reason. `sent:false` is reported rather than a false success, so a
		// downstream condition can branch on it.
		log.Printf("[workflow][node:%s][run:%s] action_send_text: not delivered on channel %q for entry=%s",
			ctx.Node.ID, ctx.Run.ID, ctx.Run.EntryType, ctx.Run.EntryID)
		return &workflow.NodeResult{
			Output: map[string]interface{}{"text": text, "sent": false, "reason": "channel_declined"},
		}, nil
	}

	log.Printf("[workflow][node:%s][run:%s] action_send_text: sent on %s to entry=%s via account=%s id=%s",
		ctx.Node.ID, ctx.Run.ID, ctx.Run.EntryType, ctx.Run.EntryID, sent.AccountID, sent.ProviderMessageID)
	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"text":       text,
			"sent":       true,
			"message_id": sent.ProviderMessageID,
			// Kept under the historical key so existing workflows reading
			// business_phone_id keep working; it is the channel account id.
			"business_phone_id": sent.AccountID,
		},
	}, nil
}
