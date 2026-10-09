package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/workspace"
)

const mediaWaitLimit = 3 * time.Minute

type MediaGeneration interface {
	Check(ctx context.Context, req mediagen.Request) error
	CheckContent(req mediagen.Request) error
	Generates(ctx context.Context, kind mediagen.Kind, model string) (bool, error)
	DefaultModel(ctx context.Context, kind mediagen.Kind) (mediagen.Model, error)
	Sources(req mediagen.Request) ([]mediagen.Source, error)
	Request(ctx context.Context, req mediagen.Request, requestedBy string) (*mediagen.Job, error)
	Wait(ctx context.Context, workspaceID, id string) (*mediagen.Job, error)
}

var mediaFailureMessages = map[mediagen.FailureCode]string{
	mediagen.FailureGeneration:           "o provedor não conseguiu gerar; tente outra descrição ou outro modelo",
	mediagen.FailureStorage:              "o resultado foi gerado mas não pôde ser salvo na biblioteca de mídia; tente de novo",
	mediagen.FailureTimedOut:             "a geração demorou demais e foi cancelada; tente de novo",
	mediagen.FailureEnqueue:              "não foi possível colocar o pedido na fila; tente de novo em instantes",
	mediagen.FailureInsufficientFunds:    "o saldo acabou antes da geração; o usuário precisa adicionar saldo",
	mediagen.FailureReferenceUnavailable: "uma mídia usada não está mais na biblioteca ou não é do tipo certo; peça outra ao usuário",
	mediagen.FailureCostUnreported:       "o provedor não informou o custo, então nada foi entregue nem cobrado; tente de novo",
}

const (
	mediaNoModel       = "nenhum modelo foi escolhido; proponha de novo para o usuário escolher o modelo no cartão de aprovação"
	mediaFailedUnknown = "não foi possível gerar; tente de novo"
	mediaStillRunning  = "ainda está sendo gerado ou aguardando o custo do provedor; peça de novo com os mesmos dados em instantes para receber o mesmo resultado sem nova cobrança"
)

var deliveredAs = map[mediagen.Kind]copilot.MediaKind{
	mediagen.KindImage: copilot.MediaImage,
	mediagen.KindMusic: copilot.MediaAudio,
	mediagen.KindVoice: copilot.MediaAudio,
	mediagen.KindVideo: copilot.MediaVideo,
}

func mediaMeta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceMedia, Action: workspace.ActionCreate}
}

func chosenModel(args map[string]interface{}, key string) string {
	model, _ := args[key].(string)
	return strings.TrimSpace(model)
}

func modelChoice(ctx context.Context, media MediaGeneration, kind mediagen.Kind, key string, choice copilot.ChoiceKind) ([]copilot.ChoiceField, error) {
	preferred, err := media.DefaultModel(ctx, kind)
	if err != nil {
		return nil, err
	}
	return []copilot.ChoiceField{{Key: key, Kind: choice, Default: preferred.ID}}, nil
}

type mediaRequester interface {
	Request(ctx context.Context, req mediagen.Request, requestedBy string) (*mediagen.Job, error)
}

func requestForThread(ctx context.Context, media mediaRequester, cc copilot.Context, req mediagen.Request) (*mediagen.Job, error) {
	req.BillingReference = cc.ChargeReference
	return media.Request(ctx, req, cc.UserID)
}

func checkMedia(ctx context.Context, media MediaGeneration, req mediagen.Request) error {
	var err error
	if req.Kind.UsesModel() && strings.TrimSpace(req.Model) == "" {
		err = media.CheckContent(req)
	} else {
		err = media.Check(ctx, req)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	return nil
}

func generateMedia(ctx context.Context, media MediaGeneration, cc copilot.Context, tool string, req mediagen.Request) copilot.Result {
	if req.Kind.UsesModel() && strings.TrimSpace(req.Model) == "" {
		return copilot.Result{Status: copilot.StatusError, Message: mediaNoModel}
	}
	ctx, cancel := context.WithTimeout(ctx, mediaWaitLimit)
	defer cancel()
	job, err := requestForThread(ctx, media, cc, req)
	if err != nil {
		log.Printf("[copilot] %s could not queue: %v", tool, err)
		return copilot.Result{Status: copilot.StatusError, Message: mediaFailedUnknown}
	}
	finished, err := media.Wait(ctx, cc.WorkspaceID, job.ID)
	if errors.Is(err, context.DeadlineExceeded) {
		return copilot.Result{Status: copilot.StatusError, Message: mediaStillRunning}
	}
	if err != nil {
		log.Printf("[copilot] %s job %s could not be followed: %v", tool, job.ID, err)
		return copilot.Result{Status: copilot.StatusError, Message: mediaFailedUnknown}
	}
	result, ok := finished.Delivered()
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: mediaFailureMessage(finished.FailureCode)}
	}
	return copilot.Result{
		Status: copilot.StatusOK,
		Data:   map[string]interface{}{"media_id": result.MediaID, "media_url": result.MediaURL, "model": result.Model},
		Media:  &copilot.Media{Kind: deliveredAs[finished.Kind], URL: result.MediaURL, MediaID: result.MediaID, Alt: finished.Prompt},
	}
}

func mediaFailureMessage(code mediagen.FailureCode) string {
	if message, ok := mediaFailureMessages[code]; ok {
		return message
	}
	return mediaFailedUnknown
}
