package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/domain/workspace"
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
	Media    MediaGeneration
	Jobs     StudioJobs
}

const (
	studioNotHere        = "esta ferramenta só funciona com um projeto do Estúdio aberto"
	studioNoEditor       = "o editor do Estúdio não está ligado a esta conversa; peça ao usuário para abrir a Elo de dentro do projeto"
	studioProjectGone    = "o projeto aberto não foi encontrado neste workspace; peça ao usuário para reabrir o projeto"
	studioEditorSilent   = "o editor não respondeu a tempo; peça ao usuário para manter o projeto aberto nesta aba e tente de novo"
	studioEditorAbsent   = "nenhuma aba com este projeto aberto respondeu e nada foi aplicado; se o usuário está com o projeto aberto, a conexão da aba caiu por um instante e ela se reconecta sozinha: tente de novo uma vez antes de pedir algo ao usuário"
	studioInterrupted    = "a edição foi interrompida antes de o editor responder"
	studioEditorRefused  = "o editor recusou o pedido"
	studioEditorGarbled  = "o editor devolveu uma resposta que não pôde ser lida; tente de novo"
	studioTooManyRunning = "o workspace já está no limite de gerações ou processamentos ao mesmo tempo; acompanhe com studio_jobs, espere um terminar e tente de novo"
	studioSourceUnusable = "a mídia deste item não serve para este processamento ou não está mais na biblioteca"
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

func runOnEditor(ctx context.Context, cc copilot.Context, name copilot.ScreenCommandName, args interface{}) (copilot.ScreenReply, error) {
	reply, err := cc.Screen.Run(ctx, copilot.ScreenCommand{Name: name, Args: args})
	switch {
	case errors.Is(err, copilot.ErrScreenUnavailable):
		return reply, errors.New(studioEditorSilent)
	case err != nil && ctx.Err() != nil:
		return reply, errors.New(studioInterrupted)
	case err != nil:
		log.Printf("[copilot] studio %s could not reach the editor: %v", name, err)
		return reply, errors.New(studioEditorSilent)
	case reply.Declined():
		log.Printf("[copilot] studio %s: only tabs without the editor answered", name)
		return reply, errors.New(studioEditorAbsent)
	case !reply.OK:
		return reply, errors.New(studioEditorRefused + ": " + reply.Error.Message)
	}
	return reply, nil
}

func screenData(reply copilot.ScreenReply) interface{} {
	if len(reply.Data) == 0 {
		return nil
	}
	return json.RawMessage(reply.Data)
}

func studioRequestFailure(tool string, err error) string {
	var invalid *mediagen.ValidationError
	switch {
	case errors.Is(err, mediagen.ErrTooManyActive):
		return studioTooManyRunning
	case errors.As(err, &invalid):
		return studioSourceUnusable
	}
	log.Printf("[copilot] %s could not queue: %v", tool, err)
	return mediaFailedUnknown
}

func StudioTools(deps StudioDeps) []copilot.Tool {
	return []copilot.Tool{
		NewStudioReadTool(deps), NewStudioLookTool(deps),
		NewStudioEditVideoTool(deps), NewStudioEditImageTool(deps),
		NewStudioStartJobTool(deps), NewStudioJobsTool(deps),
		NewStudioGenerateMusicTool(deps), NewStudioGenerateVoiceoverTool(deps), NewStudioGenerateImageTool(deps),
		NewStudioAskTool(),
	}
}
