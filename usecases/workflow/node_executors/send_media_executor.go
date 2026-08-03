package node_executors

import (
	"context"
	"log"
	"strings"
	"vozko/domain/workflow"
)

type sendMediaExecutor struct {
	// sender stays for the media-library lookup, which is channel-neutral and
	// lives on the WhatsApp sender's deps.
	sender *whatsappSender
	// channel routes the actual send, so every channel with an adapter can
	// attach media from a workflow.
	channel *channelSender
}

func NewSendMediaExecutor(waDeps SenderDeps) workflow.NodeExecutor {
	return &sendMediaExecutor{
		sender:  newWhatsAppSender(waDeps),
		channel: newChannelSender(waDeps),
	}
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
	// Any channel with a registered adapter can attach media; only a channel
	// with no send path at all is skipped, and it is skipped loudly rather than
	// reporting a success the customer never saw.
	if !e.channel.Supports(ctx.Run) {
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

	sent, err := e.channel.SendMedia(context.Background(), ctx.Run, mediaURL, caption)
	if err != nil {
		log.Printf("[workflow][node:%s][run:%s] send_media error: %v", ctx.Node.ID, ctx.Run.ID, err)
		return fail(mediaURL), nil
	}
	// A nil result with no error is a deliberate decline, most often a closed
	// outbound window. Reporting sent=true there would tell the workflow the
	// customer received something they did not.
	if sent == nil {
		return fail(mediaURL), nil
	}

	messageID := sent.ProviderMessageID

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
