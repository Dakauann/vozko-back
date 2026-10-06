package copilottools

import (
	"context"
	"log"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/tools"
)

const (
	PreviewImageReferences = "image_references"
	imageModelChoiceKey    = "image_model"
)

type ImageReferencesPreview struct {
	References []ImageReferencePreview `json:"references"`
}

type ImageReferencePreview struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
}

type generateImageArgs struct {
	Prompt            string   `json:"prompt" req:"true" desc:"descrição da imagem: assunto, cena, estilo, luz; evite pedir texto escrito na imagem"`
	Aspect            string   `json:"aspect" req:"true" enum:"square,portrait,story,landscape" desc:"square (1:1, 1080x1080), portrait (4:5, 1080x1350), story (9:16, 1080x1920) ou landscape (16:9, 1920x1080)"`
	ReferenceMediaIDs []string `json:"reference_media_ids" id:"true" desc:"opcional, até 16 media_id de imagens de referência para editar ou seguir o estilo, na ordem de importância: use os media_id dos anexos de imagem que o usuário enviou na conversa ou de imagens da biblioteca; nunca invente"`
}

func (a generateImageArgs) request(cc copilot.Context, model string) mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: cc.WorkspaceID, Model: model, Prompt: a.Prompt, Aspect: mediagen.Aspect(a.Aspect), ReferenceMediaIDs: a.ReferenceMediaIDs}
}

type generateImageTool struct{ media MediaGeneration }

func NewGenerateImageTool(media MediaGeneration) copilot.Tool {
	return &generateImageTool{media: media}
}

func (t *generateImageTool) Meta() copilot.Meta { return mediaMeta() }

func (t *generateImageTool) Definition() tools.Definition {
	return definition("generate_image",
		"Gera uma imagem com IA a partir de uma descrição, opcionalmente guiada por imagens de referência (reference_media_ids: logo, print de tela ou foto anexados pelo usuário, ou uma imagem gerada antes para ajustá-la), "+
			"salva na biblioteca de mídia e devolve media_id e media_url "+
			"(o media_id serve para create_ad, render_video e para enviar a imagem). Se o modelo desta conversa gera imagens, ele mesmo gera; senão o usuário escolhe o modelo de imagem no cartão de aprovação, "+
			"nunca nos argumentos. É cobrada do saldo pelo custo do provedor, por isso só depois da aprovação do usuário.",
		generateImageArgs{})
}

func (t *generateImageTool) Choices(ctx context.Context, cc copilot.Context, _ map[string]interface{}) ([]copilot.ChoiceField, error) {
	generates, err := t.media.Generates(ctx, mediagen.KindImage, cc.Model)
	if err != nil || generates {
		return nil, err
	}
	return []copilot.ChoiceField{{Key: imageModelChoiceKey, Kind: copilot.ChoiceImageModel}}, nil
}

func (t *generateImageTool) model(ctx context.Context, cc copilot.Context, args map[string]interface{}) (string, error) {
	if chosen := chosenModel(args, imageModelChoiceKey); chosen != "" {
		return chosen, nil
	}
	generates, err := t.media.Generates(ctx, mediagen.KindImage, cc.Model)
	if err != nil || !generates {
		return "", err
	}
	return cc.Model, nil
}

func (t *generateImageTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[generateImageArgs](nil, cc, args)
	if err != nil {
		return err
	}
	model, err := t.model(ctx, cc, args)
	if err != nil {
		return err
	}
	return checkMedia(ctx, t.media, a.request(cc, model))
}

func (t *generateImageTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a generateImageArgs
	bindArgs(args, &a)
	fields := []copilot.Field{
		{Key: "image", Value: a.Prompt},
		{Key: "format", Value: a.Aspect},
	}
	if model, err := t.model(ctx, cc, args); err == nil && model != "" {
		fields = append(fields, copilot.Field{Key: imageModelChoiceKey, Value: model})
	}
	return append(fields, copilot.Field{Key: "cost", Value: "cobrado do saldo pelo custo do provedor"})
}

func (t *generateImageTool) Preview(_ context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a generateImageArgs
	bindArgs(args, &a)
	if len(a.ReferenceMediaIDs) == 0 {
		return nil
	}
	refs, err := t.media.Sources(a.request(cc, ""))
	if err != nil {
		return nil
	}
	data := ImageReferencesPreview{References: make([]ImageReferencePreview, 0, len(refs))}
	for _, ref := range refs {
		data.References = append(data.References, ImageReferencePreview{MediaID: ref.MediaID, URL: ref.URL})
	}
	return &copilot.Preview{Kind: PreviewImageReferences, Data: data}
}

func (t *generateImageTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a generateImageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	model, err := t.model(ctx, cc, args)
	if err != nil {
		log.Printf("[copilot] generate_image could not resolve the image model: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: mediaFailedUnknown}
	}
	return generateMedia(ctx, t.media, cc, "generate_image", a.request(cc, model))
}
