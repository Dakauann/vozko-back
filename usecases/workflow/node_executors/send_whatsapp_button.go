package node_executors

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

// Special output handles of the interactive prompt node. Per-option handles use
// the option's own ID; these three are the fixed catch-alls.
const (
	interactiveHandleNoMatch    = "no_match"
	interactiveHandleNoReply    = "no_reply"
	interactiveHandleSendFailed = "send_failed"
)

const interactiveDefaultTimeoutSeconds = 300

type sendWhatsappButtonExecutor struct {
	sender *whatsappSender
}

func NewSendWhatsappButtonExecutor(waDeps WhatsAppSenderDeps) workflow.NodeExecutor {
	return &sendWhatsappButtonExecutor{sender: newWhatsAppSender(waDeps)}
}

func (e *sendWhatsappButtonExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionSendWhatsappButton,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:       "Enviar Botões ou Lista",
		Description: "Envia botões (até 3) ou uma lista (até 10 opções) e ramifica o fluxo conforme a opção escolhida pelo contato.",
		Icon:        "PaperPlaneTilt",
		Guidance: workflow.NodeGuidance{
			When: "Para enviar uma escolha ÚNICA no WhatsApp e seguir um caminho diferente por opção. Use 'buttons' para até 3 opções, 'list' para até 10.",
			Behavior: "Saídas DINÂMICAS: cada opção (botão ou linha da lista) vira uma saída com o MESMO id da opção. Preencha as opções ANTES de conectar. " +
				"Ao conectar qualquer saída de opção (ou 'no_match'/'no_reply'/'send_failed'), o nó PAUSA aguardando a resposta e ramifica pelo id escolhido. " +
				"Saídas opcionais: 'no_match' (respondeu algo fora das opções), 'no_reply' (não respondeu até o timeout), 'send_failed' (falha no envio). " +
				"Sem nenhuma saída de opção conectada, comporta-se como envio simples e segue adiante (compatibilidade).",
			Examples: []string{
				"config: {\"interactive_type\":\"buttons\",\"body\":\"Deseja continuar?\",\"buttons\":\"[{\\\"Type\\\":\\\"reply\\\",\\\"ID\\\":\\\"sim\\\",\\\"Title\\\":\\\"Sim\\\"},{\\\"Type\\\":\\\"reply\\\",\\\"ID\\\":\\\"nao\\\",\\\"Title\\\":\\\"Não\\\"}]\"}  // saídas: \"sim\", \"nao\", \"no_match\", \"no_reply\", \"send_failed\"",
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
				{Value: "buttons", Label: "Botões (até 3)"},
				{Value: "list", Label: "Lista (até 10)"},
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

// listRowConfig / listSectionConfig mirror the `sections` config JSON authored in
// the frontend. Row ID is the stable routing key (never interpolated).
type listRowConfig struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type listSectionConfig struct {
	Title string          `json:"title"`
	Rows  []listRowConfig `json:"rows"`
}

func interactiveTypeOf(config map[string]interface{}) string {
	t, _ := config["interactive_type"].(string)
	if strings.TrimSpace(strings.ToLower(t)) == "list" {
		return "list"
	}
	return "buttons"
}

func parseButtonsConfig(config map[string]interface{}) []conversation.InteractiveButton {
	raw, _ := config["buttons"].(string)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var buttons []conversation.InteractiveButton
	if err := json.Unmarshal([]byte(raw), &buttons); err != nil {
		return nil
	}
	return buttons
}

func parseListSectionsConfig(config map[string]interface{}) []listSectionConfig {
	raw, _ := config["sections"].(string)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var sections []listSectionConfig
	if err := json.Unmarshal([]byte(raw), &sections); err != nil {
		return nil
	}
	return sections
}

// AskInteractiveOutputs resolves the dynamic output handles of the interactive
// prompt node: one handle per option (id == option id, so runtime routing keys
// off the WhatsApp reply id) plus the three optional catch-alls. It is the twin
// of TextMatchOutputs and is registered in builderHandleResolver so the builder
// lint, activation, and /workflows/resolve-handles all agree.
//
// Each per-option handle is REQUIRED: a tapped button/list row must route
// somewhere, so an unconnected option fails activation (and shows the required
// marker on the canvas). The three catch-alls (no_match / no_reply / send_failed)
// are optional. A pre-existing fire-and-forget send_whatsapp_button node (single
// default edge) keeps RUNNING via the legacy wiring bridge (isInteractiveWiring /
// resolveInteractiveReplyEdge), but must wire its options to re-activate.
func AskInteractiveOutputs(config map[string]interface{}) []workflow.HandleDefinition {
	outputs := make([]workflow.HandleDefinition, 0, 6)
	seen := make(map[string]struct{})
	appendOption := func(id, label string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		outputs = append(outputs, workflow.HandleDefinition{ID: id, Label: label})
	}

	if interactiveTypeOf(config) == "list" {
		for _, section := range parseListSectionsConfig(config) {
			for _, row := range section.Rows {
				appendOption(row.ID, row.Title)
			}
		}
	} else {
		for _, b := range parseButtonsConfig(config) {
			if b.Type == conversation.ButtonTypeCopyCode {
				continue
			}
			appendOption(b.ID, b.Title)
		}
	}

	outputs = append(outputs,
		workflow.HandleDefinition{ID: interactiveHandleNoMatch, Label: "Sem correspondência", Optional: true},
		workflow.HandleDefinition{ID: interactiveHandleNoReply, Label: "Não respondeu", Optional: true},
		workflow.HandleDefinition{ID: interactiveHandleSendFailed, Label: "Falha no envio", Optional: true},
	)
	return outputs
}

// isInteractiveWiring reports whether the node is wired to branch on the reply:
// at least one outgoing edge targets an option handle or a catch-all. This is
// what distinguishes the interactive (send + park + branch) mode from the legacy
// fire-and-forget send. It never depends on a hidden flag — behavior follows the
// wiring the author drew.
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

func interactiveTimeoutSeconds(config map[string]interface{}) float64 {
	if secs, ok := config["timeout_seconds"].(float64); ok && secs > 0 {
		return secs
	}
	if mins, ok := config["timeout_minutes"].(float64); ok && mins > 0 {
		return mins * 60
	}
	return interactiveDefaultTimeoutSeconds
}

func (e *sendWhatsappButtonExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
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
		// Prefer an explicit send_failed branch; otherwise surface the error so
		// the engine's retry/backoff applies (we do NOT park on a failed send —
		// the contact never received anything to reply to).
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
		// Legacy fire-and-forget: send and continue down the single default edge.
		return &workflow.NodeResult{Output: output}, nil
	}

	// Interactive: park until the contact replies. The reply resumes via
	// AdvanceOnReply (routes by selected_option_id); a timeout resumes via
	// resumeRunFromCurrent (routes to no_reply). Reuses WaitReasonReply so the
	// existing entry-based reply router picks this run up unchanged.
	timeout := interactiveTimeoutSeconds(ctx.Node.Config)
	wakeAt := time.Now().UTC().Add(time.Duration(timeout * float64(time.Second))).UnixMilli()
	return &workflow.NodeResult{
		Output: output,
		Wait: &workflow.WaitInstruction{
			WakeAt: wakeAt,
			Reason: workflow.WaitReasonReply,
		},
	}, nil
}

// send dispatches the buttons or list message and returns the sent message id.
// A nil sender (simulation / unconfigured) is a no-op success so branching can
// still be exercised. It keeps NO state on the executor — executors are shared
// singletons across concurrent runs.
func (e *sendWhatsappButtonExecutor) send(ctx *workflow.NodeContext, body, footer string) (string, error) {
	if e.sender == nil {
		log.Printf("[workflow][node:%s][run:%s] interactive: no WhatsApp sender configured, skipping actual send",
			ctx.Node.ID, ctx.Run.ID)
		return "", nil
	}

	if interactiveTypeOf(ctx.Node.Config) == "list" {
		return e.sendList(ctx, body, footer)
	}
	return e.sendButtons(ctx, body, footer)
}

func (e *sendWhatsappButtonExecutor) sendButtons(ctx *workflow.NodeContext, body, footer string) (string, error) {
	buttons := parseButtonsConfig(ctx.Node.Config)
	if len(buttons) == 0 {
		return "", workflow.ErrNodeConfigMissing
	}
	// Interpolate user-facing titles; keep ids literal (they are routing keys).
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

func (e *sendWhatsappButtonExecutor) sendList(ctx *workflow.NodeContext, body, footer string) (string, error) {
	sectionsCfg := parseListSectionsConfig(ctx.Node.Config)
	sections := make([]conversation.ListSection, 0, len(sectionsCfg))
	for _, sc := range sectionsCfg {
		rows := make([]conversation.ListRow, 0, len(sc.Rows))
		for _, r := range sc.Rows {
			rows = append(rows, conversation.ListRow{
				ID:          r.ID, // literal routing key
				Title:       workflow.Interpolate(r.Title, ctx.State, nil),
				Description: workflow.Interpolate(r.Description, ctx.State, nil),
			})
		}
		sections = append(sections, conversation.ListSection{
			Title: workflow.Interpolate(sc.Title, ctx.State, nil),
			Rows:  rows,
		})
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

func (e *sendWhatsappButtonExecutor) applyHeader(ctx *workflow.NodeContext, input *conversation.SendButtonMessageInput) {
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
