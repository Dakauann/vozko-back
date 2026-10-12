package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/workspace"
	"vozko/infra/ai/openrouter"
	"vozko/infra/config"
	"vozko/usecases/agentloop"
	copilotuc "vozko/usecases/copilot"
	"vozko/usecases/copilot/copilottools/adtools"
	"vozko/usecases/copilot/copilottools/skilltools"
	"vozko/usecases/copilot/copilottools/studiotools"
	"vozko/usecases/copilot/skills"
)

const (
	evalWorkspace  = "eval-workspace"
	evalProject    = "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"
	evalArtboard   = "ab-1"
	fakeIDSuffix   = "-eval"
	closedBriefTag = " O brief está fechado: não faça perguntas, crie a peça direto na prancheta ab-1 (1080 x 1350) e termine com um resumo curto."
)

var briefs = map[string]string{
	"simples":   "Post para o café Grão Nobre anunciando o café da manhã de sábado, das 8h às 11h, com um botão Reserve sua mesa. Visual limpo e acolhedor.",
	"avancado":  "Arte de academia para o plano mensal de R$ 49,90, com fundo escuro em degradê, um cartão de preço com profundidade realista, brilho atrás e acabamento de grão. Título Treine sem limite.",
	"curvas":    "Arte de lançamento da coleção Verão leve de uma loja de roupas, cheia de curvas: formas orgânicas, uma onda no rodapé e uma linha curva que leva o olho do título ao chamado.",
	"diagonais": "Peça de Black Friday com energia de velocidade: uma faixa inclinada atravessando a arte, título no mesmo ângulo, linhas de velocidade e a data Só até domingo.",
}

type evalRun struct {
	Brief     string            `json:"brief"`
	Model     string            `json:"model"`
	Batches   []json.RawMessage `json:"batches"`
	ToolCalls []string          `json:"tool_calls"`
	CostUSD   float64           `json:"cost_usd"`
	Outcome   string            `json:"outcome"`
}

type usageLedger struct {
	mu     sync.Mutex
	micros int64
}

func (l *usageLedger) Publish(_ string, message []byte) error {
	var event ai.AICompletedEvent
	if err := json.Unmarshal(message, &event); err != nil {
		return err
	}
	l.mu.Lock()
	l.micros += event.ProviderCostMicros
	l.mu.Unlock()
	return nil
}

func (l *usageLedger) PublishWithDelay(topic string, message []byte, _ time.Duration) error {
	return l.Publish(topic, message)
}

func (l *usageLedger) ValidateConnection() error { return nil }

func (l *usageLedger) take() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	spent := l.micros
	l.micros = 0
	return spent
}

type evalEditor struct {
	batches []json.RawMessage
	layers  []map[string]interface{}
}

func (e *evalEditor) Run(_ context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	switch cmd.Name {
	case copilot.ScreenRead:
		return ok(map[string]interface{}{"active_artboard": evalArtboard, "artboards": []interface{}{map[string]interface{}{"id": evalArtboard, "name": "Arte", "width": 1080, "height": 1350, "background": "#ffffff", "layers": e.layers}}})
	case copilot.ScreenEdit:
		return e.edit(cmd.Args)
	}
	return copilot.ScreenReply{OK: false, Error: &copilot.ScreenError{Code: "eval", Message: "esta simulação só lê e edita; a captura e os outros comandos ficam fora da avaliação"}}, nil
}

func (e *evalEditor) edit(args interface{}) (copilot.ScreenReply, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return copilot.ScreenReply{}, err
	}
	var payload struct {
		Operations []map[string]interface{} `json:"operations"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return copilot.ScreenReply{}, err
	}
	e.batches = append(e.batches, raw)
	created := map[string][]string{}
	for _, op := range payload.Operations {
		ref, _ := op["ref"].(string)
		if ref == "" {
			continue
		}
		created[ref] = []string{ref + fakeIDSuffix}
		layer := map[string]interface{}{"id": ref + fakeIDSuffix}
		for _, field := range []string{"op", "text", "shape", "x", "y", "w", "h", "rotation"} {
			if value, found := op[field]; found {
				layer[field] = value
			}
		}
		e.layers = append(e.layers, layer)
	}
	return ok(map[string]interface{}{"applied": len(payload.Operations), "created": created, "checks": []string{}})
}

func ok(data interface{}) (copilot.ScreenReply, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return copilot.ScreenReply{}, err
	}
	return copilot.ScreenReply{OK: true, Data: raw}, nil
}

type evalProjects struct{ kind studio.Kind }

func (p evalProjects) Get(_ context.Context, workspaceID, id string) (*studio.Project, error) {
	return &studio.Project{ID: id, WorkspaceID: workspaceID, Kind: p.kind, Name: "Avaliação"}, nil
}

type allowAll struct{}

func (allowAll) Execute(string, string, workspace.Resource, workspace.Action) error { return nil }

func (allowAll) Check(string) error { return nil }

func evalRegistry(kind studio.Kind) (*copilotuc.Registry, error) {
	library, err := skills.Load()
	if err != nil {
		return nil, err
	}
	all := studiotools.StudioTools(studiotools.StudioDeps{Projects: evalProjects{kind: kind}})
	all = append(all, adtools.AdsTools(adtools.AdsDeps{})...)
	all = append(all, skilltools.NewLoadSkillTool(library))
	return copilotuc.NewRegistry(all...), nil
}

func runBrief(ctx context.Context, engine *agentloop.Engine, ledger *usageLedger, model, name string) (evalRun, error) {
	registry, err := evalRegistry(studio.KindImage)
	if err != nil {
		return evalRun{}, err
	}
	editor := &evalEditor{}
	cc := copilot.Context{
		WorkspaceID:     evalWorkspace,
		UserID:          "eval-user",
		Role:            workspace.RoleOwner,
		View:            copilot.View{Surface: copilot.SurfaceStudio, ProjectID: evalProject, ProjectKind: copilot.StudioImage},
		Screen:          editor,
		Mode:            copilot.ModeEdit,
		ChargeReference: "elo-design-eval:" + uuid.NewString(),
	}
	driver := copilotuc.NewDriver(cc, model, registry, allowAll{}, allowAll{}, uuid.NewString)
	cfg := copilotuc.DefaultConfig(cc, ai.ModelInfo{})
	cfg.AsOf = time.Now()
	session := &agentloop.Session{}
	outcome := engine.Run(ctx, func(string, interface{}) {}, driver, cfg, session, agentloop.UserRequest(briefs[name]+closedBriefTag))
	var calls []string
	for _, message := range session.History {
		for _, call := range message.ToolCalls {
			calls = append(calls, call.Name)
		}
	}
	result := "done"
	if outcome.Halt != nil {
		result = outcome.Halt.Error()
	}
	return evalRun{Brief: name, Model: model, Batches: editor.batches, ToolCalls: calls, CostUSD: float64(ledger.take()) / 1e6, Outcome: result}, nil
}

func main() {
	model := flag.String("model", "anthropic/claude-sonnet-4", "OpenRouter model id Elo runs on")
	only := flag.String("briefs", "simples,avancado,curvas,diagonais", "comma separated briefs to run")
	out := flag.String("out", "elo-design-eval.json", "where to write the runs for the offline grader")
	perception := flag.String("perception", "", "perception eval: the inputs file written by vozko-front scripts/elo-perception export")
	models := flag.String("models", "", "perception eval: comma separated models")
	reasoning := flag.String("reasoning", "", "perception eval: reasoning level sent to every call, empty for the model default")
	spent := flag.Float64("spent", -1, "perception eval: US$ already spent by earlier perception runs, counted against the cap")
	capUSD := flag.Float64("cap", 10, "perception eval and design bench: total US$ cap, earlier runs included")
	runCeilingUSD := flag.Float64("run-ceiling", 5, "design bench: US$ one run may spend before it is asked to wrap up, also reserved per worker")
	bench := flag.String("bench", "", "design bench: the inputs file written by vozko-front scripts/elo-bench (bun run elo:bench render)")
	judge := flag.String("judge", "", "design bench judge: the judge inputs file written by bun run elo:bench grade")
	judgeModel := flag.String("judge-model", "", "design bench judge: the vision model that grades, different from the designers")
	front := flag.String("front", "../vozko-front", "design bench: the vozko-front folder that holds the headless editor")
	tracks := flag.String("tracks", "replicate,brief", "design bench: comma separated tracks (replicate, brief)")
	arms := flag.String("arms", "on,off", "design bench: perception arms to run (on, off)")
	items := flag.String("items", "", "design bench: comma separated track:item entries to run (track:* for a whole track); empty runs everything")
	counted := flag.String("counted", "", "design bench: other results files whose recorded cost counts against the cap")
	workers := flag.Int("workers", 1, "design bench and judge: parallel runs; the design bench caps a model the catalog does not mark low price at 3")
	label := flag.String("label", "", "design bench: a round label kept on every run, so the same item, model and arm can run again in a later round")
	flag.Parse()
	if err := godotenv.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "no .env in the working directory; run from vozko-back")
		os.Exit(1)
	}
	ledger := &usageLedger{}
	references := newReferenceLedger(ledger)
	service, err := openrouter.NewService(openrouter.ConfigFrom(config.LoadConfig()), nil, references)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *perception != "" {
		os.Exit(perceptionMain(service, ledger, *perception, *out, strings.Split(*models, ","), ai.ParseReasoningLevel(*reasoning), *spent, *capUSD))
	}
	if *bench != "" {
		os.Exit(benchMain(service, references, benchFlags{Inputs: *bench, Out: *out, Front: *front, Models: splitList(*models), Tracks: splitList(*tracks), Arms: splitList(*arms), Only: splitList(*items), Counted: splitList(*counted), Label: *label, Cap: *capUSD, CeilingUSD: *runCeilingUSD, Workers: *workers}))
	}
	if *judge != "" {
		os.Exit(judgeMain(service, references, *judge, *out, *judgeModel, designersIn(splitList(*counted)), splitList(*counted), *capUSD, *workers))
	}
	engine := &agentloop.Engine{AI: service}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var runs []evalRun
	total := 0.0
	for _, name := range strings.Split(*only, ",") {
		name = strings.TrimSpace(name)
		if _, known := briefs[name]; !known {
			fmt.Fprintf(os.Stderr, "unknown brief %q\n", name)
			os.Exit(1)
		}
		run, err := runBrief(ctx, engine, ledger, *model, name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		total += run.CostUSD
		fmt.Printf("%s: %d batches, %d tool calls, US$ %.4f, %s\n", name, len(run.Batches), len(run.ToolCalls), run.CostUSD, run.Outcome)
		runs = append(runs, run)
	}
	raw, err := json.MarshalIndent(runs, "", "  ")
	if err == nil {
		err = os.WriteFile(*out, raw, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("total US$ %.4f; grade with: cd vozko-front && ELO_EVAL_RUNS=%s bun x vitest run src/lib/studio/agent/evals/design-briefs.live.test.ts\n", total, *out)
}
