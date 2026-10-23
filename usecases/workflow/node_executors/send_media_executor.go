package node_executors

import (
	"context"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type sendMediaExecutor struct {
	sender  *whatsappSender
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
		Type:              workflow.NodeTypeActionSendMedia,
		NeedsConversation: true,
		TestBehavior:      workflow.NodeTestSendsMessage,
		Category:          workflow.NodeCategoryAction,
		Scopes:            []workflow.NodeScope{workflow.NodeScopeWhatsApp},
		Label:             "Enviar Mídia",
		Description:       "Envia uma imagem, vídeo, áudio ou documento ao contato.",
		Icon:              "Image",
		Guidance: workflow.NodeGuidance{
			When: "Para enviar imagem, vídeo, áudio ou documento.",
		},
		DefaultConfig: map[string]interface{}{
			"media_id":  "",
			"media_url": "",
			"caption":   "",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "media_url", Description: "URL da mídia enviada", Label: "Link da mídia", Type: workflow.VariableTypeText},
			{Key: "media_type", Description: "Tipo da mídia (image, video, audio, document)", Label: "Tipo da mídia", Type: workflow.VariableTypeText},
			{Key: "caption", Description: "Legenda enviada", Label: "Legenda enviada", Type: workflow.VariableTypeText},
			{Key: "message_id", Description: "ID da mensagem enviada", Label: "ID da mensagem", Type: workflow.VariableTypeText},
			{Key: "sent", Description: "true se enviado com sucesso", Label: "Enviado", Type: workflow.VariableTypeBoolean},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "media_id", Label: "Mídia Salva", Type: "select", OptionsSource: "medias"},
			{Key: "media_url", Label: "URL da Mídia", Type: "text", Placeholder: "https://... ou {{last.image_url}}"},
			{Key: "caption", Label: "Legenda", Type: "textarea", Placeholder: "Legenda da mídia"},
		},
	}
}

func (e *sendMediaExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
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
	caption = workflow.CustomerText(ctx, caption)

	fail := func(mediaURL string) *workflow.NodeResult {
		return &workflow.NodeResult{
			Output: map[string]interface{}{"media_url": mediaURL, "media_type": "", "caption": caption, "message_id": "", "sent": false},
		}
	}

	if mediaURL == "" {
		m, err := conversation.SendableLibraryMedia(e.library(), ctx.Run.WorkspaceID, mediaID)
		if err != nil {
			log.Printf("[workflow][node:%s][run:%s] send_media: media %q is not sendable in workspace %s: %v",
				ctx.Node.ID, ctx.Run.ID, mediaID, ctx.Run.WorkspaceID, err)
			return fail(""), nil
		}
		mediaURL = strings.TrimSpace(m.URL)
	} else if err := mediaURLsOf(e.sender).SendableURL(runMediaScope(ctx.Run), mediaURL); err != nil {
		log.Printf("[workflow][node:%s][run:%s] send_media: url %q is not sendable in this conversation: %v",
			ctx.Node.ID, ctx.Run.ID, mediaURL, err)
		return fail(""), nil
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
		if failsTheStep(err) {
			return nil, err
		}
		return fail(mediaURL), nil
	}
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

func (e *sendMediaExecutor) library() conversation.LibraryMediaReader {
	if e.sender == nil || e.sender.deps.MediaRepo == nil {
		return nil
	}
	return e.sender.deps.MediaRepo
}

func mediaURLsOf(sender *whatsappSender) *conversation.MediaURLResolver {
	if sender == nil {
		return nil
	}
	return sender.deps.MediaURLs
}

func runMediaScope(run *workflow.WorkflowRun) conversation.MediaSendScope {
	return conversation.MediaSendScope{WorkspaceID: run.WorkspaceID, EntryID: run.EntryID, EntryType: shared.EntryType(run.EntryType)}
}
