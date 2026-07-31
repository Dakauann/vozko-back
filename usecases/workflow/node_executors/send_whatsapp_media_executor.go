package node_executors

import (
	"context"
	"log"
	"strings"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type sendMediaExecutor struct {
	sender *whatsappSender
}

func NewSendMediaExecutor(waDeps SenderDeps) workflow.NodeExecutor {
	return &sendMediaExecutor{sender: newWhatsAppSender(waDeps)}
}

func (e *sendMediaExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionSendMedia,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:       "Enviar Mídia",
		Description: "Envia uma imagem, vídeo, áudio ou documento ao contato.",
		Icon:        "Image",
		Guidance: workflow.NodeGuidance{
			When: "Para enviar imagem, vídeo, áudio ou documento.",
		},
		DefaultConfig: map[string]interface{}{
			"media_id":  "",
			"media_url": "",
			"caption":   "",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "media_url", Description: "URL da mídia enviada"},
			{Key: "media_type", Description: "Tipo da mídia (image, video, audio, document)"},
			{Key: "caption", Description: "Legenda enviada"},
			{Key: "message_id", Description: "ID da mensagem enviada"},
			{Key: "sent", Description: "true se enviado com sucesso"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "media_id", Label: "Mídia Salva", Type: "select", OptionsSource: "medias"},
			{Key: "media_url", Label: "URL da Mídia", Type: "text", Placeholder: "https://... ou {{last.image_url}}"},
			{Key: "caption", Label: "Legenda", Type: "text", Placeholder: "Legenda da mídia"},
		},
	}
}

func (e *sendMediaExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	// Media sending still resolves a WhatsApp media id / business phone. Adapters
	// expose SendMedia, so this node can be generalized next; until then it is
	// skipped on other channels rather than silently reporting success.
	if shared.EntryType(ctx.Run.EntryType) != shared.EntryTypeWhatsApp {
		return skipUnsupportedNode(ctx, "action_send_media"), nil
	}
	mediaID, _ := ctx.Node.Config["media_id"].(string)
	mediaID = strings.TrimSpace(workflow.Interpolate(mediaID, ctx.State, nil))
	mediaURL, _ := ctx.Node.Config["media_url"].(string)
	mediaURL = strings.TrimSpace(workflow.Interpolate(mediaURL, ctx.State, nil))
	if mediaID == "" && mediaURL == "" {
		return nil, workflow.ErrNodeConfigMissing
	}

	caption, _ := ctx.Node.Config["caption"].(string)
	caption = workflow.Interpolate(caption, ctx.State, nil)

	fail := func(mediaURL string) *workflow.NodeResult {
		return &workflow.NodeResult{
			Output: map[string]interface{}{"media_url": mediaURL, "media_type": "", "caption": caption, "message_id": "", "sent": false},
		}
	}

	if mediaURL == "" {

		if e.sender == nil || e.sender.deps.MediaRepo == nil {
			log.Printf("[workflow][node:%s][run:%s] send_media: no sender/mediaRepo configured, skipping",
				ctx.Node.ID, ctx.Run.ID)
			return &workflow.NodeResult{
				Output: map[string]interface{}{"media_url": "", "media_type": "", "caption": caption, "message_id": "", "sent": true},
			}, nil
		}

		m, err := e.sender.deps.MediaRepo.GetMediaByID(mediaID)
		if err != nil || m == nil {
			log.Printf("[workflow][node:%s][run:%s] send_media: media %q not found: %v", ctx.Node.ID, ctx.Run.ID, mediaID, err)
			return fail(""), nil
		}

		mediaURL = strings.TrimSpace(m.URL)
		if mediaURL == "" {
			log.Printf("[workflow][node:%s][run:%s] send_media: media %q has no URL", ctx.Node.ID, ctx.Run.ID, mediaID)
			return fail(""), nil
		}
	}

	if e.sender == nil {
		log.Printf("[workflow][node:%s][run:%s] send_media: no sender/mediaRepo configured, skipping",
			ctx.Node.ID, ctx.Run.ID)
		return &workflow.NodeResult{
			Output: map[string]interface{}{"media_url": mediaURL, "media_type": detectMediaType(mediaURL), "caption": caption, "message_id": "", "sent": true},
		}, nil
	}

	mediaType := detectMediaType(mediaURL)

	output, _, err := e.sender.SendMedia(context.Background(), ctx.Run, mediaURL, caption)
	if err != nil {
		log.Printf("[workflow][node:%s][run:%s] send_media error: %v", ctx.Node.ID, ctx.Run.ID, err)
		return fail(mediaURL), nil
	}

	messageID := ""
	if output != nil {
		messageID = output.MessageID
	}

	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"media_url":  mediaURL,
			"media_type": mediaType,
			"caption":    caption,
			"message_id": messageID,
			"sent":       true,
		},
	}, nil
}
