package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/workspace"
	"vozko/usecases/agentloop"
	copilotuc "vozko/usecases/copilot"
)

const (
	benchHardCap      = 35.0
	benchMinBackOff   = 120 * time.Second
	benchRunTimeout   = 25 * time.Minute
	benchDocTimeout   = 30 * time.Second
	trackReplicate    = "replicate"
	trackBrief        = "brief"
	perceptionOn      = "on"
	perceptionOff     = "off"
	benchClosedPrompt = " O pedido está fechado: não faça perguntas, trabalhe direto no projeto aberto e termine com um resumo curto."
	benchNoGeneration = " Não gere imagens, vídeos nem sons com IA nesta peça: construa tudo com camadas editáveis (texto, formas, vetores, ícones, gráficos, efeitos e animação)."
)

type benchOpen struct {
	Kind       string `json:"kind"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Aspect     string `json:"aspect,omitempty"`
	Background string `json:"background,omitempty"`
}

type benchPhoto struct {
	MediaID string `json:"media_id"`
	Subject string `json:"subject"`
}

type benchReference struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Kind   string       `json:"kind"`
	Tier   string       `json:"tier,omitempty"`
	Open   benchOpen    `json:"open"`
	Image  string       `json:"image"`
	Photos []benchPhoto `json:"photos"`
}

type benchBrief struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Tier   string       `json:"tier,omitempty"`
	Open   benchOpen    `json:"open"`
	Text   string       `json:"text"`
	Photos []benchPhoto `json:"photos"`
}

type benchInputs struct {
	Project    string           `json:"project"`
	Fonts      []string         `json:"fonts"`
	References []benchReference `json:"references"`
	Briefs     []benchBrief     `json:"briefs"`
}

type benchJob struct {
	Track      string
	Item       string
	Model      string
	Perception string
	Label      string
	Open       benchOpen
	Prompt     string
	Image      string
}

type benchRun struct {
	Track       string          `json:"track"`
	Item        string          `json:"item"`
	Model       string          `json:"model"`
	Perception  string          `json:"perception"`
	Label       string          `json:"label,omitempty"`
	Document    json.RawMessage `json:"document"`
	ToolCalls   []string        `json:"tool_calls"`
	ModelCalls  int             `json:"model_calls"`
	Looks       int             `json:"looks"`
	Edits       int             `json:"edits"`
	FailedEdits int             `json:"failed_edits"`
	CostUSD     float64         `json:"cost_usd"`
	Outcome     string          `json:"outcome"`
	Summary     string          `json:"summary"`
}

func (j benchJob) key() string {
	return strings.Join([]string{j.Track, j.Item, j.Model, j.Perception, j.Label}, "|")
}

func (r benchRun) key() string {
	return strings.Join([]string{r.Track, r.Item, r.Model, r.Perception, r.Label}, "|")
}

func (o benchOpen) kind() (studio.Kind, copilot.StudioKind, error) {
	switch o.Kind {
	case "image":
		return studio.KindImage, copilot.StudioImage, nil
	case "video":
		return studio.KindVideo, copilot.StudioVideo, nil
	}
	return "", "", fmt.Errorf("unknown project kind %q", o.Kind)
}

func (o benchOpen) sizeText() string {
	if o.Kind == "video" {
		return "um vídeo no formato " + o.Aspect
	}
	return fmt.Sprintf("uma prancheta ab-1 de %d x %d px", o.Width, o.Height)
}

func replicationPrompt(ref benchReference, fonts []string) string {
	subject := "a peça da imagem anexada"
	if ref.Kind == "video" {
		subject = "o vídeo da folha anexada (cada quadro traz o tempo em que aparece; o vídeo tem 6 segundos)"
	}
	prompt := fmt.Sprintf("Recrie %s do zero, o mais fiel possível: mesma composição, textos, cores, tipografia, formas, sombras e efeitos%s. O projeto aberto já é %s, vazio. Fontes disponíveis: %s.%s%s",
		subject, map[bool]string{true: " e o mesmo movimento", false: ""}[ref.Kind == "video"], ref.Open.sizeText(), strings.Join(fonts, ", "), benchNoGeneration, benchClosedPrompt)
	return withLibraryPhotos(prompt, ref.Photos)
}

func briefPrompt(brief benchBrief) string {
	return withLibraryPhotos(fmt.Sprintf("%s O projeto aberto já é %s, vazio.%s%s", brief.Text, brief.Open.sizeText(), benchNoGeneration, benchClosedPrompt), brief.Photos)
}

func withLibraryPhotos(prompt string, photos []benchPhoto) string {
	attachments := make([]copilot.Attachment, 0, len(photos))
	for _, p := range photos {
		attachments = append(attachments, copilot.Attachment{MediaID: p.MediaID, Name: p.Subject, Kind: "image"})
	}
	return copilot.PromptWithAttachments(prompt, attachments, true)
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func wanted(only []string, track, item string) bool {
	if len(only) == 0 {
		return true
	}
	for _, entry := range only {
		if entry == track+":"+item || entry == track+":*" {
			return true
		}
	}
	return false
}

func benchJobs(inputs benchInputs, dir string, tracks, models, arms, only []string) ([]benchJob, error) {
	var jobs []benchJob
	for _, arm := range arms {
		if arm != perceptionOn && arm != perceptionOff {
			return nil, fmt.Errorf("perception arm must be on or off, got %q", arm)
		}
		for _, track := range tracks {
			switch track {
			case trackReplicate:
				for _, ref := range inputs.References {
					if !wanted(only, track, ref.ID) {
						continue
					}
					picture, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ref.Image)))
					if err != nil {
						return nil, err
					}
					image := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(picture)
					for _, model := range models {
						jobs = append(jobs, benchJob{Track: track, Item: ref.ID, Model: model, Perception: arm, Open: ref.Open, Prompt: replicationPrompt(ref, inputs.Fonts), Image: image})
					}
				}
			case trackBrief:
				for _, brief := range inputs.Briefs {
					if !wanted(only, track, brief.ID) {
						continue
					}
					for _, model := range models {
						jobs = append(jobs, benchJob{Track: track, Item: brief.ID, Model: model, Perception: arm, Open: brief.Open, Prompt: briefPrompt(brief)})
					}
				}
			default:
				return nil, fmt.Errorf("unknown track %q", track)
			}
		}
	}
	return jobs, nil
}

type benchSession struct {
	service    ai.Service
	ledger     *referenceLedger
	project    string
	ceilingUSD float64
}

func (s benchSession) runJob(ctx context.Context, bridge editorChannel, job benchJob, limits ai.ModelInfo) (benchRun, error) {
	run := benchRun{Track: job.Track, Item: job.Item, Model: job.Model, Perception: job.Perception, Label: job.Label}
	projectKind, viewKind, err := job.Open.kind()
	if err != nil {
		return run, err
	}
	spec, err := json.Marshal(job.Open)
	if err != nil {
		return run, err
	}
	opening, cancel := context.WithTimeout(ctx, benchDocTimeout)
	err = bridge.open(opening, s.project, spec)
	cancel()
	if err != nil {
		return run, err
	}
	registry, err := evalRegistry(projectKind)
	if err != nil {
		return run, err
	}
	editor := &benchEditor{bridge: bridge, project: s.project}
	cc := copilot.Context{
		WorkspaceID:     evalWorkspace,
		UserID:          "eval-user",
		Role:            workspace.RoleOwner,
		View:            copilot.View{Surface: copilot.SurfaceStudio, ProjectID: s.project, ProjectKind: viewKind},
		Screen:          editor,
		SeesImages:      limits.SeesImages(),
		Mode:            copilot.ModeEdit,
		ChargeReference: "elo-design-bench:" + uuid.NewString(),
	}
	driver := copilotuc.NewDriver(cc, job.Model, registry, allowAll{}, allowAll{}, uuid.NewString)
	cfg := copilotuc.DefaultConfig(cc, limits)
	cfg.CostCeilingMicros = int64(s.ceilingUSD * 1e6)
	cfg.AsOf = time.Now()
	session := &agentloop.Session{}
	if job.Image != "" {
		session.PromptImages = []string{job.Image}
	}
	working, stop := context.WithTimeout(ctx, benchRunTimeout)
	outcome := (&agentloop.Engine{AI: s.service}).Run(working, func(string, interface{}) {}, driver, cfg, session, job.Prompt)
	stop()
	for _, message := range session.History {
		if message.Role == ai.RoleAssistant {
			run.ModelCalls++
		}
		for _, call := range message.ToolCalls {
			run.ToolCalls = append(run.ToolCalls, call.Name)
		}
	}
	run.Looks, run.Edits, run.FailedEdits = editor.tally.looks, editor.tally.edits, editor.tally.failed
	run.CostUSD = float64(max(s.ledger.takeFor(cc.ChargeReference), session.CostMicros)) / 1e6
	run.Summary = outcome.Summary
	run.Outcome = "done"
	if outcome.Halt != nil {
		run.Outcome = outcome.Halt.Error()
	}
	reading, done := context.WithTimeout(ctx, benchDocTimeout)
	run.Document, err = bridge.document(reading)
	done()
	if err != nil {
		return run, err
	}
	if errors.Is(outcome.Halt, agentloop.ErrProviderFailed) {
		return run, outcome.Halt
	}
	return run, nil
}

type benchPlan struct {
	Jobs     []benchJob
	Cap      float64
	Spent    float64
	Reserve  float64
	Wait     func(time.Duration)
	Save     func([]benchRun) error
	Limits   func(model string) (ai.ModelInfo, error)
	Bridge   func(perception string) (editorChannel, error)
	Run      func(ctx context.Context, bridge editorChannel, job benchJob, limits ai.ModelInfo) (benchRun, error)
	Progress func(benchRun)
}

func spentOnRuns(runs []benchRun) float64 {
	total := 0.0
	for _, r := range runs {
		total += r.CostUSD
	}
	return total
}

func runBench(ctx context.Context, plan benchPlan, done *[]benchRun) string {
	finished := map[string]bool{}
	for _, r := range *done {
		finished[r.key()] = true
	}
	var bridge editorChannel
	arm := ""
	defer func() {
		if bridge != nil {
			bridge.close()
		}
	}()
	dearest := plan.Reserve
	for _, job := range plan.Jobs {
		if finished[job.key()] {
			continue
		}
		spent := plan.Spent + spentOnRuns(*done)
		if spent+dearest > plan.Cap {
			return fmt.Sprintf("stopped before the US$ %.2f cap (spent US$ %.4f, the next run may cost up to US$ %.2f)", plan.Cap, spent, dearest)
		}
		limits, err := plan.Limits(job.Model)
		if err != nil {
			return fmt.Sprintf("stopped: %s cannot design here: %v", job.Model, err)
		}
		if bridge == nil || !bridge.usable() || arm != job.Perception {
			if bridge != nil {
				bridge.close()
			}
			bridge, err = plan.Bridge(job.Perception)
			if err != nil {
				return fmt.Sprintf("stopped: %v", err)
			}
			arm = job.Perception
		}
		run, err := plan.Run(ctx, bridge, job, limits)
		if wait, busy := ai.BusyRetryAfter(err); busy {
			lost := run.CostUSD
			*done = append(*done, run)
			if saveErr := plan.Save(*done); saveErr != nil {
				return fmt.Sprintf("stopped: the results could not be saved: %v", saveErr)
			}
			plan.Wait(max(wait, benchMinBackOff))
			run, err = plan.Run(ctx, bridge, job, limits)
			run.CostUSD += lost
			*done = (*done)[:len(*done)-1]
		}
		*done = append(*done, run)
		if saveErr := plan.Save(*done); saveErr != nil {
			return fmt.Sprintf("stopped: the results could not be saved: %v", saveErr)
		}
		if plan.Progress != nil {
			plan.Progress(run)
		}
		dearest = max(dearest, run.CostUSD)
		if errors.Is(err, ai.ErrProviderBusy) {
			return fmt.Sprintf("stopped: the provider is still busy after one back off (%v)", err)
		}
		if err != nil {
			return fmt.Sprintf("stopped on %s: %v", job.key(), err)
		}
	}
	return ""
}
