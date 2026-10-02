package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/imagegen"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

const imageWaitLimit = 3 * time.Minute

const PreviewImageReferences = "image_references"

type ImageReferencesPreview struct {
	References []ImageReferencePreview `json:"references"`
}

type ImageReferencePreview struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
}

type ImageGeneration interface {
	Check(req imagegen.Request) error
	References(workspaceID string, ids []string) ([]imagegen.ReferenceImage, error)
	Request(ctx context.Context, req imagegen.Request, requestedBy string) (*imagegen.Job, error)
	Wait(ctx context.Context, workspaceID, id string) (*imagegen.Job, error)
}

var imageFailureMessages = map[imagegen.FailureCode]string{
	imagegen.FailureGeneration:           "o provedor não conseguiu gerar a imagem; tente outra descrição",
	imagegen.FailureStorage:              "a imagem foi gerada mas não pôde ser salva na biblioteca de mídia; tente de novo",
	imagegen.FailureTimedOut:             "a geração da imagem demorou demais e foi cancelada; tente de novo",
	imagegen.FailureEnqueue:              "não foi possível colocar a imagem na fila; tente de novo em instantes",
	imagegen.FailureInsufficientFunds:    "o saldo acabou antes da geração; o usuário precisa adicionar saldo",
	imagegen.FailureReferenceUnavailable: "uma imagem de referência não está mais na biblioteca de mídia ou não é uma imagem; peça outra ao usuário",
}

const (
	imageFailedUnknown = "não foi possível gerar a imagem; tente de novo"
	imageStillRunning  = "a imagem ainda está sendo gerada; peça de novo com a mesma descrição e o mesmo formato em instantes para receber a mesma imagem sem nova cobrança"
)

type generateImageArgs struct {
	Prompt            string   `json:"prompt" req:"true" desc:"descrição da imagem: assunto, cena, estilo, luz; evite pedir texto escrito na imagem"`
	Aspect            string   `json:"aspect" req:"true" enum:"square,portrait,story" desc:"square (1:1, 1080x1080), portrait (4:5, 1080x1350) ou story (9:16, 1080x1920)"`
	ReferenceMediaIDs []string `json:"reference_media_ids" id:"true" desc:"opcional, até 16 media_id de imagens de referência para editar ou seguir o estilo, na ordem de importância: use os media_id dos anexos de imagem que o usuário enviou na conversa ou de imagens da biblioteca; nunca invente"`
}

func (a generateImageArgs) request(cc copilot.Context) imagegen.Request {
	return imagegen.Request{WorkspaceID: cc.WorkspaceID, Prompt: a.Prompt, Aspect: imagegen.Aspect(a.Aspect), ReferenceMediaIDs: a.ReferenceMediaIDs}
}

type generateImageTool struct{ images ImageGeneration }

func NewGenerateImageTool(images ImageGeneration) copilot.Tool {
	return &generateImageTool{images: images}
}

func (t *generateImageTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceMedia, Action: workspace.ActionCreate}
}

func (t *generateImageTool) Definition() tools.Definition {
	return definition("generate_image",
		"Gera uma imagem com IA a partir de uma descrição, opcionalmente guiada por imagens de referência (reference_media_ids, por exemplo fotos anexadas pelo usuário), "+
			"salva na biblioteca de mídia e devolve media_id e media_url "+
			"(o media_id serve para create_ad e para enviar a imagem). É cobrada do saldo como uso de IA, por isso só depois da aprovação do usuário.",
		generateImageArgs{})
}

func (t *generateImageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[generateImageArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if err := t.images.Check(a.request(cc)); err != nil {
		return fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	return nil
}

func (t *generateImageTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a generateImageArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "image", Value: a.Prompt},
		{Key: "format", Value: a.Aspect},
		{Key: "cost", Value: "cobrado do saldo como uso de IA"},
	}
}

func (t *generateImageTool) Preview(_ context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a generateImageArgs
	bindArgs(args, &a)
	if len(a.ReferenceMediaIDs) == 0 {
		return nil
	}
	refs, err := t.images.References(cc.WorkspaceID, a.ReferenceMediaIDs)
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
	ctx, cancel := context.WithTimeout(ctx, imageWaitLimit)
	defer cancel()
	job, err := t.images.Request(ctx, a.request(cc), cc.UserID)
	if err != nil {
		log.Printf("[copilot] generate_image could not queue: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: imageFailedUnknown}
	}
	finished, err := t.images.Wait(ctx, cc.WorkspaceID, job.ID)
	if errors.Is(err, context.DeadlineExceeded) {
		return copilot.Result{Status: copilot.StatusError, Message: imageStillRunning}
	}
	if err != nil {
		log.Printf("[copilot] generate_image job %s could not be followed: %v", job.ID, err)
		return copilot.Result{Status: copilot.StatusError, Message: imageFailedUnknown}
	}
	if finished.Status != imagegen.StatusDone {
		return copilot.Result{Status: copilot.StatusError, Message: imageFailureMessage(finished.FailureCode)}
	}
	return copilot.Result{
		Status: copilot.StatusOK,
		Data:   map[string]interface{}{"media_id": finished.MediaID, "media_url": finished.MediaURL, "model": finished.Model},
		Image:  &copilot.Image{URL: finished.MediaURL, MediaID: finished.MediaID, Alt: finished.Prompt},
	}
}

func imageFailureMessage(code imagegen.FailureCode) string {
	if message, ok := imageFailureMessages[code]; ok {
		return message
	}
	return imageFailedUnknown
}
