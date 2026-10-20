package node_executors

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const (
	interactiveHandleNoMatch    = workflow.InteractiveHandleNoMatch
	interactiveHandleNoReply    = workflow.InteractiveHandleNoReply
	interactiveHandleSendFailed = workflow.InteractiveHandleSendFailed
)

const interactiveDefaultTimeoutSeconds = 300

type interactivePromptExecutor struct {
	sender  *whatsappSender
	channel *channelSender
}

func NewInteractivePromptExecutor(waDeps SenderDeps) workflow.NodeExecutor {
	return &interactivePromptExecutor{
		sender:  newWhatsAppSender(waDeps),
		channel: newChannelSender(waDeps),
	}
}

func (e *interactivePromptExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:          workflow.NodeTypeActionSendInteractive,
		TestBehavior:  workflow.NodeTestWaits,
		Category:      workflow.NodeCategoryAction,
		Scopes:        []workflow.NodeScope{workflow.NodeScopeShared},
		Label:         "Enviar Opções",
		Description:   "Envia uma escolha única (botões ou lista) e ramifica o fluxo conforme a opção escolhida pelo contato. Funciona no WhatsApp, Instagram e Telegram, cada canal exibe as opções no formato que suporta.",
		Icon:          "PaperPlaneTilt",
		ChannelLimits: e.channelLimits(),
		Guidance: workflow.NodeGuidance{
			When: "Para pedir uma escolha ÚNICA e seguir um caminho diferente por opção. Use 'buttons' para poucas opções e 'list' para muitas; cada canal renderiza no formato nativo dele.",
			Behavior: "Saídas DINÂMICAS: cada opção (botão ou linha da lista) vira uma saída com o MESMO id da opção. Preencha as opções ANTES de conectar. " +
				"Ao conectar qualquer saída de opção (ou 'no_match'/'no_reply'/'send_failed'), o nó PAUSA aguardando a resposta e ramifica pelo id escolhido. " +
				"Saídas opcionais: 'no_match' (respondeu algo fora das opções), 'no_reply' (não respondeu até o timeout), 'send_failed' (falha no envio). " +
				"Sem nenhuma saída de opção conectada, comporta-se como envio simples e segue adiante (compatibilidade). " +
				"LIMITES POR CANAL: WhatsApp exibe 3 botões ou 10 itens de lista (com descrição por item); Instagram exibe até 13 opções com rótulo de 20 caracteres e SEM descrição; " +
				"Telegram exibe as opções em teclado inline, sem limite prático de quantidade nem de rótulo, mas o id da opção não pode passar de 64 bytes. " +
				"Opções além do limite de um canal simplesmente não aparecem nele, o editor mostra quais. " +
				"FORMATO EXATO das opções, com as chaves escritas exatamente assim: buttons é uma string JSON com a lista de {\"Type\":\"reply\",\"ID\":\"...\",\"Title\":\"...\",\"CopyCode\":\"\"}; " +
				"sections é uma string JSON com a lista de {\"title\":\"...\",\"rows\":[{\"id\":\"...\",\"title\":\"...\",\"description\":\"...\"}]} (minúsculas).",
			Examples: []string{
				"config: {\"interactive_type\":\"buttons\",\"body\":\"Deseja continuar?\",\"buttons\":\"[{\\\"Type\\\":\\\"reply\\\",\\\"ID\\\":\\\"sim\\\",\\\"Title\\\":\\\"Sim\\\"},{\\\"Type\\\":\\\"reply\\\",\\\"ID\\\":\\\"nao\\\",\\\"Title\\\":\\\"Não\\\"}]\"}  // saídas: \"sim\", \"nao\", \"no_match\", \"no_reply\", \"send_failed\"",
				"config: {\"interactive_type\":\"list\",\"body\":\"Qual assunto?\",\"list_button\":\"Ver opções\",\"sections\":\"[{\\\"title\\\":\\\"Assuntos\\\",\\\"rows\\\":[{\\\"id\\\":\\\"horarios\\\",\\\"title\\\":\\\"Horários\\\",\\\"description\\\":\\\"\\\"},{\\\"id\\\":\\\"valores\\\",\\\"title\\\":\\\"Valores\\\",\\\"description\\\":\\\"\\\"}]}]\"}  // saídas: \"horarios\", \"valores\", \"no_match\", \"no_reply\", \"send_failed\"",
			},
		},
		DynamicHandles: true,
		DefaultConfig: map[string]interface{}{
			"interactive_type": "buttons",
			"body":             "Seu texto aqui",
			"buttons":          "[]",
			"list_button":      "Ver opções",
			"sections":         "[]",
			"header_type":      "",
			"header_text":      "",
			"header_media_url": "",
			"footer":           "",
			"timeout_seconds":  float64(interactiveDefaultTimeoutSeconds),
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "message_id", Description: "ID da mensagem enviada"},
			{Key: "sent", Description: "true se enviado com sucesso"},
			{Key: "selected_option_id", Description: "id da opção escolhida (após a resposta)"},
			{Key: "selected_option_title", Description: "texto da opção escolhida (após a resposta)"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "interactive_type", Label: "Formato", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "buttons", Label: "Botões (poucas opções)"},
				{Value: "list", Label: "Lista (muitas opções)"},
			}},
			{Key: "header_type", Label: "Tipo de Cabeçalho", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "", Label: "Nenhum"},
				{Value: "text", Label: "Texto"},
				{Value: "image", Label: "Imagem"},
				{Value: "video", Label: "Vídeo"},
			}},
			{Key: "header_text", Label: "Texto do Cabeçalho", Type: "text", Placeholder: "Título da mensagem (apenas para cabeçalho de texto)"},
			{Key: "header_media_url", Label: "URL da Mídia", Type: "text", Placeholder: "https://exemplo.com/imagem.jpg"},
			{Key: "body", Label: "Texto da mensagem", Type: "textarea", Placeholder: "Escreva a mensagem que acompanhará as opções", Required: true},
			{Key: "footer", Label: "Rodapé", Type: "text", Placeholder: "Texto do rodapé (opcional)"},
			{Key: "buttons", Label: "Botões", Type: "buttons"},
			{Key: "list_button", Label: "Rótulo do menu (lista)", Type: "text", Placeholder: "Ver opções"},
			{Key: "sections", Label: "Opções da lista", Type: "list_sections"},
			{Key: "timeout_seconds", Label: "Timeout (segundos)", Type: "number", Placeholder: "300"},
		},
	}
}

func isListPrompt(config map[string]interface{}) bool {
	return workflow.InteractivePromptStyle(config) == channel.InteractiveStyleList
}

func invalidInteractiveConfig(err error) error {
	return fmt.Errorf("%w: %v", workflow.ErrNodeInvalidInteractiveConfig, err)
}

func AskInteractiveOutputs(config map[string]interface{}) []workflow.HandleDefinition {
	return append(workflow.InteractiveOptionHandles(config),
		workflow.HandleDefinition{ID: interactiveHandleNoMatch, Label: "Sem correspondência", Optional: true},
		workflow.HandleDefinition{ID: interactiveHandleNoReply, Label: "Não respondeu", Optional: true},
		workflow.HandleDefinition{ID: interactiveHandleSendFailed, Label: "Falha no envio", Optional: true},
	)
}

func isInteractiveWiring(edges []workflow.Edge, config map[string]interface{}) bool {
	branchLabels := make(map[string]struct{})
	for _, h := range AskInteractiveOutputs(config) {
		branchLabels[h.ID] = struct{}{}
	}
	for _, edge := range edges {
		if _, ok := branchLabels[edge.Label]; ok {
			return true
		}
	}
	return false
}

func PromptTimeoutSeconds(config map[string]interface{}) float64 {
	if secs, ok := config["timeout_seconds"].(float64); ok && secs > 0 {
		return secs
	}
	if mins, ok := config["timeout_minutes"].(float64); ok && mins > 0 {
		return mins * 60
	}
	return interactiveDefaultTimeoutSeconds
}

func (e *interactivePromptExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if !e.channel.SupportsInteractive(ctx.Run) {
		return skipUnsupportedNode(ctx, "action_send_interactive"), nil
	}
	body, _ := ctx.Node.Config["body"].(string)
	if strings.TrimSpace(body) == "" {
		return nil, workflow.ErrNodeConfigMissing
	}
	body = workflow.Interpolate(body, ctx.State, nil)

	footer := workflow.Interpolate(stringConfig(ctx.Node.Config, "footer"), ctx.State, nil)
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	interactive := isInteractiveWiring(edges, ctx.Node.Config)

	messageID, sendErr := e.send(ctx, body, footer)
	if sendErr != nil {
		log.Printf("[workflow][node:%s][run:%s] interactive send error: %v", ctx.Node.ID, ctx.Run.ID, sendErr)
		if target := resolveEdgeByLabelStrict(edges, interactiveHandleSendFailed); target != "" {
			return &workflow.NodeResult{
				NextNodeID: target,
				Output:     map[string]interface{}{"body": body, "message_id": "", "sent": false},
			}, nil
		}
		return nil, sendErr
	}

	output := map[string]interface{}{"body": body, "message_id": messageID, "sent": true}

	if !interactive {
		return &workflow.NodeResult{Output: output}, nil
	}

	timeout := PromptTimeoutSeconds(ctx.Node.Config)
	wakeAt := time.Now().UTC().Add(time.Duration(timeout * float64(time.Second))).UnixMilli()
	return &workflow.NodeResult{
		Output: output,
		Wait: &workflow.WaitInstruction{
			WakeAt: wakeAt,
			Reason: workflow.WaitReasonReply,
		},
	}, nil
}

func (e *interactivePromptExecutor) send(ctx *workflow.NodeContext, body, footer string) (string, error) {
	if shared.EntryType(ctx.Run.EntryType) == shared.EntryTypeWhatsApp {
		if e.sender == nil {
			log.Printf("[workflow][node:%s][run:%s] interactive: no WhatsApp sender configured, skipping actual send",
				ctx.Node.ID, ctx.Run.ID)
			return "", nil
		}
		if isListPrompt(ctx.Node.Config) {
			return e.sendList(ctx, body, footer)
		}
		return e.sendButtons(ctx, body, footer)
	}

	return e.sendViaAdapter(ctx, body, footer)
}

func (e *interactivePromptExecutor) sendViaAdapter(ctx *workflow.NodeContext, body, footer string) (string, error) {
	options, err := interactiveOptionsOf(ctx.Node.Config, ctx.State)
	if err != nil {
		return "", err
	}
	if len(options) == 0 {
		return "", workflow.ErrNodeConfigMissing
	}

	sent, err := e.channel.SendInteractive(context.Background(), ctx.Run, conversation.SendInteractiveRequest{
		Body:    body,
		Header:  workflow.Interpolate(stringConfig(ctx.Node.Config, "header_text"), ctx.State, nil),
		Footer:  footer,
		Options: options,
		Style:   workflow.InteractivePromptStyle(ctx.Node.Config),
	})
	if err != nil {
		return "", err
	}
	if sent == nil {
		return "", nil
	}
	return sent.ProviderMessageID, nil
}

func interactiveOptionsOf(config map[string]interface{}, state *workflow.RunState) ([]conversation.InteractiveOption, error) {
	var out []conversation.InteractiveOption
	appendOption := func(id, title string) {
		if strings.TrimSpace(id) == "" {
			return
		}
		out = append(out, conversation.InteractiveOption{
			ID:    id,
			Title: workflow.Interpolate(title, state, nil),
		})
	}

	if isListPrompt(config) {
		sections, err := workflow.InteractiveListSections(config)
		if err != nil {
			return nil, invalidInteractiveConfig(err)
		}
		for _, section := range sections {
			for _, row := range section.Rows {
				appendOption(row.ID, row.Title)
			}
		}
		return out, nil
	}

	buttons, err := workflow.InteractiveButtons(config)
	if err != nil {
		return nil, invalidInteractiveConfig(err)
	}
	for _, b := range buttons {
		appendOption(b.ID, b.Title)
	}
	return out, nil
}

func (e *interactivePromptExecutor) sendButtons(ctx *workflow.NodeContext, body, footer string) (string, error) {
	buttons, err := workflow.InteractiveButtons(ctx.Node.Config)
	if err != nil {
		return "", invalidInteractiveConfig(err)
	}
	if len(buttons) == 0 {
		return "", workflow.ErrNodeConfigMissing
	}
	for i := range buttons {
		buttons[i].Title = workflow.Interpolate(buttons[i].Title, ctx.State, nil)
	}

	input := conversation.SendButtonMessageInput{
		BodyText:   body,
		FooterText: footer,
		Buttons:    buttons,
	}
	e.applyHeader(ctx, &input)

	out, _, err := e.sender.SendButtonsWithInput(context.Background(), ctx.Run, input)
	if err != nil {
		return "", err
	}
	if out != nil {
		return out.MessageID, nil
	}
	return "", nil
}

func (e *interactivePromptExecutor) sendList(ctx *workflow.NodeContext, body, footer string) (string, error) {
	sections, err := workflow.InteractiveListSections(ctx.Node.Config)
	if err != nil {
		return "", invalidInteractiveConfig(err)
	}
	for i := range sections {
		sections[i].Title = workflow.Interpolate(sections[i].Title, ctx.State, nil)
		for j := range sections[i].Rows {
			row := &sections[i].Rows[j]
			row.Title = workflow.Interpolate(row.Title, ctx.State, nil)
			row.Description = workflow.Interpolate(row.Description, ctx.State, nil)
		}
	}

	buttonLabel := strings.TrimSpace(workflow.Interpolate(stringConfig(ctx.Node.Config, "list_button"), ctx.State, nil))
	if buttonLabel == "" {
		buttonLabel = "Ver opções"
	}

	input := conversation.SendListMessageInput{
		BodyText:   body,
		FooterText: footer,
		ButtonText: buttonLabel,
		Sections:   sections,
	}
	if headerText := strings.TrimSpace(workflow.Interpolate(stringConfig(ctx.Node.Config, "header_text"), ctx.State, nil)); headerText != "" {
		input.HeaderText = headerText
	}

	out, _, err := e.sender.SendListWithInput(context.Background(), ctx.Run, input)
	if err != nil {
		return "", err
	}
	if out != nil {
		return out.MessageID, nil
	}
	return "", nil
}

func (e *interactivePromptExecutor) applyHeader(ctx *workflow.NodeContext, input *conversation.SendButtonMessageInput) {
	headerType, _ := ctx.Node.Config["header_type"].(string)
	if headerType == "" {
		return
	}
	input.HeaderType = conversation.HeaderType(headerType)
	switch conversation.HeaderType(headerType) {
	case conversation.HeaderTypeText:
		input.HeaderText = workflow.Interpolate(stringConfig(ctx.Node.Config, "header_text"), ctx.State, nil)
	case conversation.HeaderTypeImage, conversation.HeaderTypeVideo:
		input.MediaLink = workflow.Interpolate(stringConfig(ctx.Node.Config, "header_media_url"), ctx.State, nil)
	}
}

func stringConfig(config map[string]interface{}, key string) string {
	v, _ := config[key].(string)
	return v
}

func (e *interactivePromptExecutor) channelLimits() map[string]workflow.ChannelInteractiveLimits {
	support := e.channel.InteractiveSupport()
	if len(support) == 0 {
		return nil
	}
	out := make(map[string]workflow.ChannelInteractiveLimits, len(support))
	for entryType, limits := range support {
		out[string(entryType)] = workflow.ChannelInteractiveLimits{
			MaxOptionsButtons:    limits.MaxOptionsButtons,
			MaxOptionsList:       limits.MaxOptionsList,
			MaxLabelRunes:        limits.MaxLabelRunes,
			MaxPayloadBytes:      limits.MaxPayloadBytes,
			SupportsDescriptions: limits.SupportsOptionDescriptions,
		}
	}
	return out
}
