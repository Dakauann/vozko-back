package studiotools

import (
	"context"
	"errors"
	"fmt"
	"log"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/mediatools"
	"vozko/usecases/copilot/copilottools/screencall"
)

type StudioProjects interface {
	Get(ctx context.Context, workspaceID, id string) (*studio.Project, error)
}

type StudioJobs interface {
	Get(ctx context.Context, workspaceID, id string) (*mediagen.Job, error)
	Wait(ctx context.Context, workspaceID, id string) (*mediagen.Job, error)
	ProcessingPriced(kind mediagen.Kind) bool
}

type StudioDeps struct {
	Projects StudioProjects
	Media    mediatools.MediaGeneration
	Jobs     StudioJobs
	Presets  StudioPresets
	Links    StudioShortLinks
	Library  StudioMediaLibrary
}

const (
	studioNotHere           = "esta ferramenta só funciona com um projeto do Estúdio aberto"
	studioNoEditor          = "o editor do Estúdio não está ligado a esta conversa; peça ao usuário para abrir a Elo de dentro do projeto"
	studioProjectGone       = "o projeto aberto não foi encontrado neste workspace; peça ao usuário para reabrir o projeto"
	studioEditorAwaited     = "A aba do projeto deve estar em segundo plano ou adormecida. Não peça ao usuário para avisar nem para deixar a aba aberta: quando o editor voltar a responder, o app avisa você nesta resposta ou retoma a conversa sozinho. Não repita o pedido agora; siga com o que não depende do editor ou encerre dizendo o que está pronto e o que falta."
	studioEditorSilent      = "o editor não respondeu a tempo e nada mudou no projeto. " + studioEditorAwaited
	studioEditorExpired     = "o editor não respondeu a tempo e nada deste pedido foi aplicado: ele expirou e não entra depois, nem quando a aba voltar. " + studioEditorAwaited
	studioEditorUnconfirmed = "o editor começou a aplicar este pedido e não confirmou o resultado a tempo; quando ele voltar a responder, confira com studio_read antes de repetir qualquer coisa. " + studioEditorAwaited
	studioEditorAbsent      = "nenhuma aba com este projeto aberto respondeu e nada foi aplicado; se o usuário está com o projeto aberto, a conexão da aba caiu por um instante e ela se reconecta sozinha: tente de novo uma vez antes de pedir algo ao usuário"
	studioEditorRefused     = "o editor recusou o pedido"
	studioEditorGarbled     = "o editor devolveu uma resposta que não pôde ser lida; tente de novo"
	studioGenerationsFull   = "o workspace já tem %d gerações com IA rodando ao mesmo tempo, o máximo (imagem, música, locução e efeito sonoro dividem essa fila); nada foi gerado nem cobrado. Espere uma terminar com studio_jobs e wait_seconds nos job_id que você já tem e peça esta de novo; gere um por vez e reaproveite uma mídia pronta em vez de gerar outra igual"
	studioProcessingFull    = "o workspace já tem %d processamentos rodando ao mesmo tempo, o máximo (legendas, limpeza de áudio e remoção de fundo dividem essa fila); nada foi iniciado. Espere um terminar com studio_jobs e wait_seconds nos job_id que você já tem e peça este de novo"
	studioSourceUnusable    = "a mídia deste item não serve para este processamento ou não está mais na biblioteca"
)

type studioScope struct{ kind copilot.StudioKind }

func (s studioScope) OfferedOn(view copilot.View) bool {
	return view.OnStudio() && (s.kind == "" || view.ProjectKind == s.kind)
}

func studioReadMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceMedia, Action: workspace.ActionRead}
}

func studioEditMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceMedia, Action: workspace.ActionCreate}
}

func studioFailure(message string) copilot.Result {
	return copilot.Result{Status: copilot.StatusError, Message: message}
}

func (d StudioDeps) openProject(ctx context.Context, cc copilot.Context) (*studio.Project, error) {
	if !cc.View.OnStudio() {
		return nil, errors.New(studioNotHere)
	}
	if cc.Screen == nil {
		return nil, errors.New(studioNoEditor)
	}
	if d.Projects == nil {
		return nil, errors.New(studioProjectGone)
	}
	project, err := d.Projects.Get(ctx, cc.WorkspaceID, cc.View.ProjectID)
	if err != nil || project == nil || string(project.Kind) != string(cc.View.ProjectKind) {
		return nil, errors.New(studioProjectGone)
	}
	return project, nil
}

var editorWords = screencall.Words{Silent: studioEditorSilent, Expired: studioEditorExpired, Unconfirmed: studioEditorUnconfirmed, Absent: studioEditorAbsent, Refused: studioEditorRefused}

func runOnEditor(ctx context.Context, cc copilot.Context, name copilot.ScreenCommandName, args interface{}) (copilot.ScreenReply, error) {
	return screencall.Run(ctx, cc, name, args, editorWords)
}

func studioLaneFull(kind mediagen.Kind) string {
	_, ceiling := kind.ActiveLane()
	if kind.UsesModel() {
		return fmt.Sprintf(studioGenerationsFull, ceiling)
	}
	return fmt.Sprintf(studioProcessingFull, ceiling)
}

func studioRequestFailure(tool string, kind mediagen.Kind, err error) string {
	var invalid *mediagen.ValidationError
	switch {
	case errors.Is(err, mediagen.ErrTooManyActive):
		return studioLaneFull(kind)
	case errors.As(err, &invalid):
		return studioSourceUnusable
	}
	log.Printf("[copilot] %s could not queue: %v", tool, err)
	return mediatools.MediaFailedUnknown
}

func StudioTools(deps StudioDeps) []copilot.Tool {
	return []copilot.Tool{
		NewStudioReadTool(deps), NewStudioLookTool(deps), NewStudioListenTool(deps), NewStudioLibrarySearchTool(),
		NewStudioEditVideoTool(deps), NewStudioEditImageTool(deps), NewStudioOpGuideTool(),
		NewStudioStartJobTool(deps), NewStudioJobsTool(deps),
		NewStudioGenerateMusicTool(deps), NewStudioGenerateVoiceoverTool(deps), NewStudioGenerateSoundEffectTool(deps), NewStudioGenerateImageTool(deps),
		NewStudioExportTool(deps),
		NewStudioAnimationPresetsTool(deps), NewStudioSaveAnimationPresetTool(deps),
		NewStudioAskTool(),
	}
}
