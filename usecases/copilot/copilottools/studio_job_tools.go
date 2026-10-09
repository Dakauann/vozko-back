package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/tools"
)

const (
	MaxStudioJobsAtOnce    = 8
	MaxStudioJobWait       = 60
	studioJobPriced        = "este processamento passou a ser cobrado; peça ao usuário para iniciá-lo pelo painel do editor, onde o custo aparece"
	studioJobNeedsClip     = "informe o clip_id do clipe de origem"
	studioJobNeedsLayer    = "em projetos de imagem só existe cutout, com o layer_id de uma camada de imagem"
	studioJobUnconfirmed   = "o trabalho foi para a fila, mas o editor não confirmou que vai colocá-lo no projeto; quando ficar pronto, insira com studio_edit usando o media_id que studio_jobs devolver"
	studioJobPlacedLater   = "o editor coloca o resultado no projeto sozinho quando ficar pronto; continue editando e acompanhe com studio_jobs"
	studioSourceUnreadable = "o editor não informou a mídia de origem; tente de novo"
)

var studioJobStatusText = map[mediagen.Status]string{
	mediagen.StatusQueued:   "na fila",
	mediagen.StatusRunning:  "gerando",
	mediagen.StatusSettling: "finalizando",
	mediagen.StatusDone:     "pronto",
	mediagen.StatusFailed:   "falhou",
}

type studioFollow struct {
	Job        studioJobView `json:"job"`
	Purpose    mediagen.Kind `json:"purpose"`
	ClipID     string        `json:"clipId,omitempty"`
	LayerID    string        `json:"layerId,omitempty"`
	TrackID    string        `json:"trackId,omitempty"`
	AtMS       *int64        `json:"atMs,omitempty"`
	DurationMS *int64        `json:"durationMs,omitempty"`
}

type studioJobView struct {
	ID     string          `json:"id"`
	Kind   mediagen.Kind   `json:"kind"`
	Status mediagen.Status `json:"status"`
}

func followOnScreen(ctx context.Context, cc copilot.Context, job *mediagen.Job, follow studioFollow) map[string]interface{} {
	follow.Job = studioJobView{ID: job.ID, Kind: job.Kind, Status: job.Status}
	data := map[string]interface{}{"job_id": job.ID, "kind": string(job.Kind), "status": studioJobStatusText[job.Status]}
	if _, err := runOnEditor(ctx, cc, copilot.ScreenFollowJob, follow); err != nil {
		data["note"] = studioJobUnconfirmed
		return data
	}
	data["note"] = studioJobPlacedLater
	return data
}

type studioStartJobArgs struct {
	Kind    string `json:"kind" req:"true" enum:"captions,denoise,cutout" desc:"captions: legendas da fala de um clipe de vídeo ou áudio, que viram uma faixa de texto; denoise: limpa o áudio de um clipe; cutout: remove o fundo de uma imagem"`
	ClipID  string `json:"clip_id" desc:"vídeo: o clipe de origem (id de studio_read)"`
	LayerID string `json:"layer_id" desc:"imagem: a camada de imagem (só cutout)"`
}

type studioStartJobTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioStartJobTool(deps StudioDeps) copilot.Tool {
	return &studioStartJobTool{deps: deps}
}

func (t *studioStartJobTool) Meta() copilot.Meta { return studioEditMeta() }

func (t *studioStartJobTool) Definition() tools.Definition {
	return definition("studio_start_job",
		"Coloca na fila um processamento do editor sobre um item do projeto aberto, pelo mesmo caminho do botão do editor: legendas (captions), "+
			"limpeza de áudio (denoise) ou remoção de fundo (cutout). Devolve o job_id na hora e não espera: o editor coloca o resultado no projeto "+
			"quando ficar pronto (legendas viram uma faixa de texto; denoise e cutout trocam a mídia do item). Continue editando o resto e acompanhe com studio_jobs.",
		studioStartJobArgs{})
}

func (t *studioStartJobTool) target(cc copilot.Context, a studioStartJobArgs) (studioFollow, error) {
	kind := mediagen.Kind(a.Kind)
	if cc.View.ProjectKind == copilot.StudioImage {
		if kind != mediagen.KindCutout || strings.TrimSpace(a.LayerID) == "" {
			return studioFollow{}, errors.New(studioJobNeedsLayer)
		}
		return studioFollow{Purpose: kind, LayerID: a.LayerID}, nil
	}
	if strings.TrimSpace(a.ClipID) == "" {
		return studioFollow{}, errors.New(studioJobNeedsClip)
	}
	return studioFollow{Purpose: kind, ClipID: a.ClipID}, nil
}

func (t *studioStartJobTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioStartJobArgs
	if err := decodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	follow, err := t.target(cc, a)
	if err != nil {
		return studioFailure(err.Error())
	}
	if t.deps.Jobs == nil || t.deps.Media == nil || t.deps.Jobs.ProcessingPriced(follow.Purpose) {
		return studioFailure(studioJobPriced)
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	source, err := resolveSource(ctx, cc, follow)
	if err != nil {
		return studioFailure(err.Error())
	}
	job, err := requestForThread(ctx, t.deps.Media, cc, mediagen.Request{Kind: follow.Purpose, WorkspaceID: cc.WorkspaceID, SourceMediaID: source})
	if err != nil {
		return studioFailure(studioRequestFailure("studio_start_job", err))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: followOnScreen(ctx, cc, job, follow)}
}

func resolveSource(ctx context.Context, cc copilot.Context, follow studioFollow) (string, error) {
	reply, err := runOnEditor(ctx, cc, copilot.ScreenResolveSource, map[string]string{"kind": string(follow.Purpose), "clipId": follow.ClipID, "layerId": follow.LayerID})
	if err != nil {
		return "", err
	}
	var source struct {
		SourceMediaID string `json:"sourceMediaId"`
	}
	if json.Unmarshal(reply.Data, &source) != nil {
		return "", errors.New(studioSourceUnreadable)
	}
	if _, err := uuid.Parse(source.SourceMediaID); err != nil {
		return "", errors.New(studioSourceUnreadable)
	}
	return source.SourceMediaID, nil
}

type studioJobsArgs struct {
	JobIDs      []string `json:"job_ids" req:"true" id:"true" desc:"job_id devolvidos por studio_start_job e studio_generate_*; até 8"`
	WaitSeconds int      `json:"wait_seconds" desc:"espera até todos terminarem, no máximo 60 segundos; 0 (padrão) só consulta"`
}

type studioJobsTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioJobsTool(deps StudioDeps) copilot.Tool {
	return &studioJobsTool{deps: deps}
}

func (t *studioJobsTool) Meta() copilot.Meta { return studioReadMeta() }

func (t *studioJobsTool) Definition() tools.Definition {
	return definition("studio_jobs",
		"Mostra o andamento dos trabalhos que você colocou na fila (legendas, limpeza, remoção de fundo, música, locução, imagem): na fila, gerando, "+
			"finalizando, pronto (com o media_id) ou falhou (com o motivo). Com wait_seconds espera até eles terminarem. Pronto não quer dizer que já "+
			"está no projeto: confira com studio_read; se o editor tiver sido fechado, insira o media_id com studio_edit.",
		studioJobsArgs{})
}

func (t *studioJobsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioJobsArgs
	if err := decodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if len(a.JobIDs) == 0 || len(a.JobIDs) > MaxStudioJobsAtOnce || a.WaitSeconds < 0 || a.WaitSeconds > MaxStudioJobWait {
		return studioFailure(fmt.Sprintf("informe de 1 a %d job_ids e wait_seconds de 0 a %d", MaxStudioJobsAtOnce, MaxStudioJobWait))
	}
	if t.deps.Jobs == nil {
		return studioFailure(mediaFailedUnknown)
	}
	if !cc.View.OnStudio() {
		return studioFailure(studioNotHere)
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(a.WaitSeconds)*time.Second)
	defer cancel()
	out := make([]map[string]interface{}, 0, len(a.JobIDs))
	for _, id := range a.JobIDs {
		job, err := t.status(ctx, waitCtx, cc.WorkspaceID, id, a.WaitSeconds > 0)
		if err != nil {
			out = append(out, map[string]interface{}{"job_id": id, "status": "não encontrado"})
			continue
		}
		out = append(out, studioJobLine(job))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"jobs": out}}
}

func (t *studioJobsTool) status(ctx, waitCtx context.Context, workspaceID, id string, wait bool) (*mediagen.Job, error) {
	if wait && waitCtx.Err() == nil {
		if job, err := t.deps.Jobs.Wait(waitCtx, workspaceID, id); err == nil {
			return job, nil
		}
	}
	return t.deps.Jobs.Get(ctx, workspaceID, id)
}

func studioJobLine(job *mediagen.Job) map[string]interface{} {
	line := map[string]interface{}{"job_id": job.ID, "kind": string(job.Kind), "status": studioJobStatusText[job.Status]}
	if result, ok := job.Delivered(); ok {
		line["media_id"] = result.MediaID
	}
	if job.Status == mediagen.StatusFailed {
		line["failure"] = mediaFailureMessage(job.FailureCode)
	}
	return line
}

type queuedGeneration interface {
	copilot.Tool
	copilot.ChoiceAsker
	copilot.Validator
	copilot.Describer
	generationRequest(ctx context.Context, cc copilot.Context, args map[string]interface{}) (mediagen.Request, error)
}

type studioPlacementArgs struct {
	AtMS       *int64 `json:"at_ms" desc:"vídeo: onde o resultado entra na linha do tempo, em ms; padrão no cursor de tempo"`
	TrackID    string `json:"track_id" desc:"vídeo: faixa preferida (id de studio_read); padrão a primeira livre do tipo certo"`
	DurationMS *int64 `json:"duration_ms" desc:"vídeo, só imagem: quanto tempo a imagem fica na tela, em ms"`
}

type studioGenerationTool struct {
	studioScope
	inner queuedGeneration
	deps  StudioDeps
	kind  mediagen.Kind
}

func NewStudioGenerateMusicTool(deps StudioDeps) copilot.Tool {
	return &studioGenerationTool{studioScope: studioScope{kind: copilot.StudioVideo}, inner: NewGenerateMusicTool(deps.Media).(queuedGeneration), deps: deps, kind: mediagen.KindMusic}
}

func NewStudioGenerateVoiceoverTool(deps StudioDeps) copilot.Tool {
	return &studioGenerationTool{studioScope: studioScope{kind: copilot.StudioVideo}, inner: NewGenerateVoiceoverTool(deps.Media).(queuedGeneration), deps: deps, kind: mediagen.KindVoice}
}

func NewStudioGenerateImageTool(deps StudioDeps) copilot.Tool {
	return &studioGenerationTool{inner: NewGenerateImageTool(deps.Media).(queuedGeneration), deps: deps, kind: mediagen.KindImage}
}

func (t *studioGenerationTool) Meta() copilot.Meta { return t.inner.Meta() }

var studioGenerationPurpose = map[mediagen.Kind]string{
	mediagen.KindMusic: "uma música instrumental curta (cerca de 30 segundos), que entra como clipe de áudio",
	mediagen.KindVoice: "uma locução do roteiro exato, que entra como clipe de áudio",
	mediagen.KindImage: "uma imagem com IA, que entra como clipe de imagem no vídeo ou como camada na arte",
}

func (t *studioGenerationTool) Definition() tools.Definition {
	inner := t.inner.Definition()
	def := definition("studio_"+inner.Name,
		"Gera "+studioGenerationPurpose[t.kind]+" do projeto aberto. É cobrada do saldo pelo custo do provedor. Conforme o modo do usuário, roda direto com o modelo recomendado ou pede aprovação (o usuário escolhe o modelo no cartão). "+
			"Vai para a fila e devolve o job_id na hora: continue editando enquanto gera; o editor coloca o resultado no lugar pedido quando ficar pronto. "+
			"Acompanhe com studio_jobs.",
		studioPlacementArgs{})
	for name, param := range inner.Parameters {
		def.Parameters[name] = param
	}
	def.Required = append(append([]string(nil), inner.Required...), def.Required...)
	return def
}

func (t *studioGenerationTool) Choices(ctx context.Context, cc copilot.Context, args map[string]interface{}) ([]copilot.ChoiceField, error) {
	choices, err := t.inner.Choices(ctx, cc, args)
	if err != nil {
		return nil, err
	}
	for i, choice := range choices {
		if choice.Default != "" {
			continue
		}
		if preferred, err := t.deps.Media.DefaultModel(ctx, t.kind); err == nil {
			choices[i].Default = preferred.ID
		}
	}
	return choices, nil
}

func (t *studioGenerationTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if _, err := t.placement(args); err != nil {
		return err
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	return t.inner.Validate(ctx, cc, args)
}

func (t *studioGenerationTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	fields := t.inner.Describe(ctx, cc, args)
	placement, err := t.placement(args)
	if err != nil || placement.AtMS == nil {
		return fields
	}
	return append(fields, copilot.Field{Key: "at", Value: secondsText(float64(*placement.AtMS) / 1000)})
}

func (t *studioGenerationTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	if previewer, ok := t.inner.(copilot.Previewer); ok {
		return previewer.Preview(ctx, cc, args)
	}
	return nil
}

func (t *studioGenerationTool) placement(args map[string]interface{}) (studioPlacementArgs, error) {
	var p studioPlacementArgs
	bindArgs(args, &p)
	switch {
	case p.AtMS != nil && (*p.AtMS < 0 || *p.AtMS > mediagen.MaxVideoMS):
		return p, fmt.Errorf("%w: at_ms fora do vídeo", errInvalidArgs)
	case p.DurationMS != nil && (*p.DurationMS < mediagen.MinClipMS || *p.DurationMS > mediagen.MaxVideoMS):
		return p, fmt.Errorf("%w: duration_ms fora dos limites", errInvalidArgs)
	}
	return p, nil
}

func (t *studioGenerationTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	placement, err := t.placement(args)
	if err != nil {
		return studioFailure(err.Error())
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	req, err := t.inner.generationRequest(ctx, cc, args)
	if err != nil {
		return studioFailure(err.Error())
	}
	if req.Kind.UsesModel() && strings.TrimSpace(req.Model) == "" {
		return studioFailure(mediaNoModel)
	}
	job, err := requestForThread(ctx, t.deps.Media, cc, req)
	if err != nil {
		return studioFailure(studioRequestFailure("studio_"+t.inner.Definition().Name, err))
	}
	follow := studioFollow{Purpose: t.kind, TrackID: placement.TrackID, AtMS: placement.AtMS, DurationMS: placement.DurationMS}
	data := followOnScreen(ctx, cc, job, follow)
	data["model"] = req.Model
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func (t *studioStartJobTool) NeedsApproval(mode copilot.Mode) bool { return mode == copilot.ModeAsk }

func (t *studioStartJobTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	var a studioStartJobArgs
	if err := decodeArgs(args, &a); err != nil {
		return err
	}
	follow, err := t.target(cc, a)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	if t.deps.Jobs == nil || t.deps.Jobs.ProcessingPriced(follow.Purpose) {
		return fmt.Errorf("%w: %s", errInvalidArgs, studioJobPriced)
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	return nil
}

func (t *studioStartJobTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a studioStartJobArgs
	bindArgs(args, &a)
	fields := []copilot.Field{{Key: "kind", Value: a.Kind}}
	return append(fields, copilot.Field{Key: "cost", Value: "sem custo"})
}

func (t *studioGenerationTool) NeedsApproval(mode copilot.Mode) bool { return mode != copilot.ModeFull }
