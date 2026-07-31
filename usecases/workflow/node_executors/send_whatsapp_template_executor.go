package node_executors

import (
	"context"
	"fmt"
	"log"
	"strings"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type sendTemplateExecutor struct {
	sender *whatsappSender
}

func NewSendTemplateExecutor(waDeps SenderDeps) workflow.NodeExecutor {
	return &sendTemplateExecutor{sender: newWhatsAppSender(waDeps)}
}

func (e *sendTemplateExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:     workflow.NodeTypeActionSendTemplate,
		Category: workflow.NodeCategoryAction,
		// Templates are the only WhatsApp message type allowed to a contact with
		// no open 24h window, so they are what a flow reaches for to re-engage.
		Scopes:      []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:       "Enviar Template",
		Description: "Envia um template pré-aprovado do WhatsApp para o contato. Selecione o número do WhatsApp emissor.",
		Icon:        "FileText",
		Guidance: workflow.NodeGuidance{
			When:     "Para enviar um template de WhatsApp pré-aprovado (necessário fora da janela de 24h). Em fluxos de voz, envia ao número da chamada.",
			Behavior: "Em fluxos VoIP o destinatário vem de {{phone_number}} (número da chamada) e o número emissor do WhatsApp precisa ser selecionado em \"Número do WhatsApp\".",
		},
		DefaultConfig: map[string]interface{}{
			"business_phone_id": "",
			"template_id":       "",
			"params":            map[string]interface{}{},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "business_phone_id", Description: "ID do número do WhatsApp usado no envio"},
			{Key: "template_id", Description: "ID do template enviado"},
			{Key: "template_name", Description: "Nome do template enviado"},
			{Key: "message_id", Description: "ID da mensagem enviada"},
			{Key: "sent", Description: "true se enviado com sucesso"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "business_phone_id", Label: "Número do WhatsApp", Type: "select", Placeholder: "Selecione um número do workspace", OptionsSource: "business_phones"},
			{Key: "template_id", Label: "Template WhatsApp", Type: "select", Placeholder: "Selecione um template do mesmo WABA", Required: true, OptionsSource: "whatsapp_templates"},
			{Key: "params", Label: "Parâmetros do Template", Type: "keyvalue", Placeholder: "nome=valor (suporta {{var.nome}})"},
		},
	}
}

func (e *sendTemplateExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	// Templates are a WhatsApp construct: no other channel has an approved,
	// pre-registered message. On any other channel the node is skipped and the
	// run continues, so a workflow shared across channels still completes.
	if shared.EntryType(ctx.Run.EntryType) != shared.EntryTypeWhatsApp {
		return skipUnsupportedNode(ctx, "action_send_template"), nil
	}
	templateID, _ := ctx.Node.Config["template_id"].(string)
	if templateID == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	templateID = workflow.Interpolate(templateID, ctx.State, nil)
	preferredBusinessPhoneID, _ := ctx.Node.Config["business_phone_id"].(string)
	preferredBusinessPhoneID = strings.TrimSpace(workflow.Interpolate(preferredBusinessPhoneID, ctx.State, nil))

	paramValues := make(map[string]string)
	if params, ok := ctx.Node.Config["params"].(map[string]interface{}); ok {
		for k, v := range params {
			paramValues[k] = fmt.Sprintf("%v", v)
		}
	}

	if e.sender == nil {
		log.Printf("[workflow][node:%s][run:%s] send_template: no WhatsApp sender configured, skipping actual send",
			ctx.Node.ID, ctx.Run.ID)
		return &workflow.NodeResult{
			Output: map[string]interface{}{"business_phone_id": preferredBusinessPhoneID, "template_id": templateID, "template_name": "", "message_id": "", "sent": true},
		}, nil
	}

	output, usedBusinessPhoneID, err := e.sender.SendTemplate(context.Background(), ctx.Run, templateID, preferredBusinessPhoneID, paramValues, ctx.State)
	if err != nil {
		log.Printf("[workflow][node:%s][run:%s] send_template error: %v", ctx.Node.ID, ctx.Run.ID, err)
		return &workflow.NodeResult{
			Output: map[string]interface{}{"business_phone_id": usedBusinessPhoneID, "template_id": templateID, "template_name": "", "message_id": "", "sent": false},
		}, nil
	}

	messageID := ""
	if output != nil {
		messageID = output.MessageID
	}

	return &workflow.NodeResult{
		Output: map[string]interface{}{"business_phone_id": usedBusinessPhoneID, "template_id": templateID, "template_name": "", "message_id": messageID, "sent": true},
	}, nil
}
