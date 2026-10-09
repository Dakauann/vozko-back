package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
)

const (
	studioProjectID = "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"
	studioSourceID  = "7a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

type fakeEditor struct {
	replies  map[copilot.ScreenCommandName]copilot.ScreenReply
	err      error
	commands []copilot.ScreenCommand
}

func (f *fakeEditor) Run(_ context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	f.commands = append(f.commands, cmd)
	if f.err != nil {
		return copilot.ScreenReply{}, f.err
	}
	if reply, ok := f.replies[cmd.Name]; ok {
		return reply, nil
	}
	return copilot.ScreenReply{OK: true, Data: []byte(`{}`)}, nil
}

func (f *fakeEditor) sent(name copilot.ScreenCommandName) (copilot.ScreenCommand, bool) {
	for _, cmd := range f.commands {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return copilot.ScreenCommand{}, false
}

type fakeProjects struct{ project *studio.Project }

func (f fakeProjects) Get(_ context.Context, workspaceID, id string) (*studio.Project, error) {
	if f.project == nil || f.project.WorkspaceID != workspaceID || f.project.ID != id {
		return nil, studio.ErrProjectNotFound
	}
	return f.project, nil
}

type fakeJobs struct {
	jobs   map[string]*mediagen.Job
	priced bool
	waited []string
}

func (f *fakeJobs) Get(_ context.Context, _, id string) (*mediagen.Job, error) {
	if job, ok := f.jobs[id]; ok {
		return job, nil
	}
	return nil, mediagen.ErrJobNotFound
}

func (f *fakeJobs) Wait(ctx context.Context, workspaceID, id string) (*mediagen.Job, error) {
	f.waited = append(f.waited, id)
	return f.Get(ctx, workspaceID, id)
}

func (f *fakeJobs) ProcessingPriced(mediagen.Kind) bool { return f.priced }

func studioSession(kind copilot.StudioKind, editor *fakeEditor) copilot.Context {
	return copilot.Context{
		WorkspaceID: "ws-1", UserID: "u-1", Model: "anthropic/claude-sonnet-4", SeesImages: true, Screen: editor,
		View: copilot.View{Surface: copilot.SurfaceStudio, ProjectID: studioProjectID, ProjectKind: kind},
	}
}

func studioDeps(kind studio.Kind, media *stubImages, jobs *fakeJobs) StudioDeps {
	return StudioDeps{Projects: fakeProjects{project: &studio.Project{ID: studioProjectID, WorkspaceID: "ws-1", Kind: kind}}, Media: media, Jobs: jobs}
}

func TestStudioReadAsksTheOpenEditor(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenRead: {OK: true, Data: []byte(`{"tracks":3}`)}}}
	res := NewStudioReadTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, editor), nil)
	if res.Status != copilot.StatusOK || renderJSON(res.Data) != `{"tracks":3}` {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenRead)
	if renderJSON(cmd.Args) != `{"detail":"summary"}` {
		t.Fatalf("args = %s", renderJSON(cmd.Args))
	}
}

func renderJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestStudioToolsFailClosedWithoutTheirProject(t *testing.T) {
	read := NewStudioReadTool(studioDeps(studio.KindVideo, nil, nil))
	cases := map[string]copilot.Context{
		"no editor attached": func() copilot.Context {
			cc := studioSession(copilot.StudioVideo, nil)
			cc.Screen = nil
			return cc
		}(),
		"another workspace": func() copilot.Context {
			cc := studioSession(copilot.StudioVideo, &fakeEditor{})
			cc.WorkspaceID = "ws-2"
			return cc
		}(),
		"wrong project kind": studioSession(copilot.StudioImage, &fakeEditor{}),
		"outside the studio": func() copilot.Context {
			cc := studioSession(copilot.StudioVideo, &fakeEditor{})
			cc.View = copilot.View{}
			return cc
		}(),
	}
	for name, cc := range cases {
		if res := read.Execute(context.Background(), cc, nil); res.Status != copilot.StatusError {
			t.Fatalf("%s: result = %+v", name, res)
		}
		if editor, ok := cc.Screen.(*fakeEditor); ok && editor != nil && len(editor.commands) > 0 {
			t.Fatalf("%s: nothing may reach the editor", name)
		}
	}
}

func TestASilentOrRefusingEditorIsExplainedToTheModel(t *testing.T) {
	read := NewStudioReadTool(studioDeps(studio.KindVideo, nil, nil))
	silent := read.Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{err: copilot.ErrScreenUnavailable}), nil)
	if silent.Status != copilot.StatusError || silent.Message != studioEditorSilent {
		t.Fatalf("silent = %+v", silent)
	}
	absent := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{
		copilot.ScreenRead: {Error: &copilot.ScreenError{Code: copilot.ScreenNoEditor, Message: "o projeto não está aberto nesta aba"}},
	}}
	if res := read.Execute(context.Background(), studioSession(copilot.StudioVideo, absent), nil); res.Message != studioEditorAbsent {
		t.Fatalf("only tabs without the editor answered, so nothing was applied and the model may retry: %+v", res)
	}
	refusing := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{
		copilot.ScreenRead: {Error: &copilot.ScreenError{Code: "busy", Message: "o usuário está recortando uma imagem"}},
	}}
	res := read.Execute(context.Background(), studioSession(copilot.StudioVideo, refusing), nil)
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "recortando") {
		t.Fatalf("refusal = %+v", res)
	}
}

func TestStudioLookOnlyWorksForAModelThatSees(t *testing.T) {
	capture := "data:image/jpeg;base64,AAAA"
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenLook: {OK: true, Data: []byte(`{"frames":[0,1500]}`), Images: []string{capture}}}}
	look := NewStudioLookTool(studioDeps(studio.KindVideo, nil, nil))
	cc := studioSession(copilot.StudioVideo, editor)
	res := look.Execute(context.Background(), cc, map[string]interface{}{"times_ms": []interface{}{0.0, 1500.0}})
	if res.Status != copilot.StatusOK || len(res.Images) != 1 || res.Images[0] != capture {
		t.Fatalf("look = %+v", res)
	}
	cc.SeesImages = false
	if res := look.Execute(context.Background(), cc, nil); res.Message != studioBlind {
		t.Fatalf("a blind model must be told, got %+v", res)
	}
	tooMany := make([]interface{}, MaxStudioLookFrames+1)
	for i := range tooMany {
		tooMany[i] = float64(i * 100)
	}
	cc.SeesImages = true
	if res := look.Execute(context.Background(), cc, map[string]interface{}{"times_ms": tooMany}); res.Status != copilot.StatusError {
		t.Fatalf("too many frames = %+v", res)
	}
	blank := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenLook: {OK: true}}}
	if res := look.Execute(context.Background(), studioSession(copilot.StudioVideo, blank), nil); res.Message != studioNoCapture {
		t.Fatalf("a look without a capture must fail, got %+v", res)
	}
}

func TestStudioEditSendsTheBatchAsWritten(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true, Data: []byte(`{"created":{"titulo":"c-9"}}`)}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_text", "ref": "titulo", "text": "Oferta", "at_ms": 0.0, "x": 0.0, "y": 0.3, "unknown": "drop"},
		map[string]interface{}{"op": "animate", "clip_id": "@titulo", "property": "scale", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 1.0}}},
	}}
	res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args)
	if res.Status != copilot.StatusOK || !strings.Contains(renderJSON(res.Data), "c-9") {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"at_ms":0`, `"x":0`, `"clip_id":"@titulo"`, `"keys":[{"at_ms":0,"value":1`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	if strings.Contains(sent, "unknown") || strings.Contains(sent, `"y":null`) {
		t.Fatalf("unknown or empty fields must not reach the editor: %s", sent)
	}
}

func TestStudioEditVideoCarriesTheImageDesignVocabulary(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_text", "text": "Oferta", "at_ms": 0.0, "gradient": map[string]interface{}{"kind": "linear", "from": "#ffffff", "to": "#ff8a00"}, "highlight": map[string]interface{}{"color": "#111111"}, "curve": 0.4},
		map[string]interface{}{"op": "add_icon", "icon_id": "heart", "at_ms": 0.0, "blend_mode": "screen"},
		map[string]interface{}{"op": "update_clip", "clip_id": "c-1", "clear": []interface{}{"gradient", "highlight", "curve", "blend_mode"}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"gradient":{`, `"highlight":{`, `"curve":0.4`, `"op":"add_icon"`, `"icon_id":"heart"`, `"blend_mode":"screen"`, `"clear":["gradient","highlight","curve","blend_mode"]`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesMalformedBatchesBeforeTheEditor(t *testing.T) {
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	many := make([]interface{}, MaxStudioOperations+1)
	for i := range many {
		many[i] = map[string]interface{}{"op": "seek", "at_ms": 0.0}
	}
	cases := map[string]map[string]interface{}{
		"no operations":         {"operations": []interface{}{}},
		"too many operations":   {"operations": many},
		"unknown operation":     {"operations": []interface{}{map[string]interface{}{"op": "export"}}},
		"image op on the video": {"operations": []interface{}{map[string]interface{}{"op": "align_layers"}}},
	}
	for name, args := range cases {
		editor := &fakeEditor{}
		if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusError {
			t.Fatalf("%s: result = %+v", name, res)
		}
		if len(editor.commands) > 0 {
			t.Fatalf("%s: nothing may reach the editor", name)
		}
	}
}

func TestStudioEditDocumentsEveryOperationItAccepts(t *testing.T) {
	for _, tool := range []copilotToolWithGuide{
		{NewStudioEditVideoTool(StudioDeps{}), opEnum(studioVideoOperation{})},
		{NewStudioEditImageTool(StudioDeps{}), opEnum(studioImageOperation{})},
	} {
		description := tool.tool.Definition().Description
		for _, op := range tool.ops {
			if !strings.Contains(description, op+"(") {
				t.Fatalf("%s does not explain %s", tool.tool.Definition().Name, op)
			}
		}
	}
}

type copilotToolWithGuide struct {
	tool copilot.Tool
	ops  []string
}

func startJobEditor() *fakeEditor {
	return &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{
		copilot.ScreenResolveSource: {OK: true, Data: []byte(`{"sourceMediaId":"` + studioSourceID + `"}`)},
	}}
}

func TestStudioStartJobQueuesThroughMediagenAndHandsTheJobToTheEditor(t *testing.T) {
	media := &stubImages{}
	editor := startJobEditor()
	tool := NewStudioStartJobTool(studioDeps(studio.KindVideo, media, &fakeJobs{}))
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"kind": "captions", "clip_id": "c-1"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	if media.requested.Kind != mediagen.KindCaptions || media.requested.SourceMediaID != studioSourceID || media.requestedBy != "u-1" {
		t.Fatalf("request = %+v by %s", media.requested, media.requestedBy)
	}
	if media.waitedFor != "" {
		t.Fatal("Elo must not wait for the job; she keeps working")
	}
	follow, ok := editor.sent(copilot.ScreenFollowJob)
	if !ok || !strings.Contains(renderJSON(follow.Args), `"id":"job-1"`) || !strings.Contains(renderJSON(follow.Args), `"clipId":"c-1"`) {
		t.Fatalf("the editor must follow the job for the clip, got %s", renderJSON(follow.Args))
	}
	if !strings.Contains(renderJSON(res.Data), "job-1") {
		t.Fatalf("the job id must reach the model, got %s", renderJSON(res.Data))
	}
}

func TestStudioStartJobFailsClosed(t *testing.T) {
	cases := map[string]struct {
		kind   copilot.StudioKind
		args   map[string]interface{}
		jobs   *fakeJobs
		editor *fakeEditor
		media  *stubImages
		want   string
	}{
		"priced processing":          {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{priced: true}, startJobEditor(), &stubImages{}, studioJobPriced},
		"no clip":                    {copilot.StudioVideo, map[string]interface{}{"kind": "denoise"}, &fakeJobs{}, startJobEditor(), &stubImages{}, studioJobNeedsClip},
		"captions on an image":       {copilot.StudioImage, map[string]interface{}{"kind": "captions", "layer_id": "l-1"}, &fakeJobs{}, startJobEditor(), &stubImages{}, studioJobNeedsLayer},
		"source that is not media":   {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{}, &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenResolveSource: {OK: true, Data: []byte(`{"sourceMediaId":"../x"}`)}}}, &stubImages{}, studioSourceUnreadable},
		"too many processing jobs":   {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{}, startJobEditor(), &stubImages{requestErr: mediagen.ErrTooManyActive}, studioTooManyRunning},
		"unknown processing request": {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{}, startJobEditor(), &stubImages{requestErr: errors.New("db down")}, mediaFailedUnknown},
	}
	for name, tc := range cases {
		kind := studio.KindVideo
		if tc.kind == copilot.StudioImage {
			kind = studio.KindImage
		}
		tool := NewStudioStartJobTool(studioDeps(kind, tc.media, tc.jobs))
		res := tool.Execute(context.Background(), studioSession(tc.kind, tc.editor), tc.args)
		if res.Status != copilot.StatusError || res.Message != tc.want {
			t.Fatalf("%s: result = %+v", name, res)
		}
		if _, followed := tc.editor.sent(copilot.ScreenFollowJob); followed {
			t.Fatalf("%s: the editor must not follow a job that was not queued", name)
		}
	}
}

func TestStudioJobsReportsPlainStatuses(t *testing.T) {
	jobs := &fakeJobs{jobs: map[string]*mediagen.Job{
		"0b9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11": {ID: "0b9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11", Kind: mediagen.KindCaptions, Status: mediagen.StatusDone, MediaID: studioSourceID},
		"1c9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11": {ID: "1c9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11", Kind: mediagen.KindMusic, Status: mediagen.StatusFailed, FailureCode: mediagen.FailureInsufficientFunds},
	}}
	tool := NewStudioJobsTool(studioDeps(studio.KindVideo, nil, jobs))
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{
		"job_ids": []interface{}{"0b9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11", "1c9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11", "2d9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11"},
	})
	out := renderJSON(res.Data)
	for _, want := range []string{`"status":"pronto"`, `"media_id":"` + studioSourceID + `"`, `"status":"falhou"`, "saldo acabou", `"status":"não encontrado"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
	if len(jobs.waited) != 0 {
		t.Fatal("without wait_seconds nothing waits")
	}
	if res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{"job_ids": []interface{}{"0b9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11"}, "wait_seconds": 61.0}); res.Status != copilot.StatusError {
		t.Fatalf("a wait past the bound must be refused, got %+v", res)
	}
}

func TestStudioGenerationReusesTheGeneratorCardAndDoesNotWait(t *testing.T) {
	media := &stubImages{}
	editor := &fakeEditor{}
	deps := studioDeps(studio.KindVideo, media, &fakeJobs{})
	tool := NewStudioGenerateMusicTool(deps)
	if !tool.Meta().Mutating {
		t.Fatal("a paid generation needs the approval card")
	}
	def := tool.Definition()
	if def.Name != "studio_generate_music" || def.Parameters["prompt"].Type == "" || def.Parameters["at_ms"].Type == "" {
		t.Fatalf("definition = %+v", def)
	}
	choices, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), studioSession(copilot.StudioVideo, editor), nil)
	if err != nil || len(choices) != 1 || choices[0].Kind != copilot.ChoiceMusicModel {
		t.Fatalf("choices = %+v, %v", choices, err)
	}
	args := map[string]interface{}{"prompt": "violão leve para café", "at_ms": 2000.0, "music_model": "google/lyria-3-clip-preview"}
	if err := tool.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioVideo, editor), args); err != nil {
		t.Fatal(err)
	}
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args)
	if res.Status != copilot.StatusOK || media.requested.Kind != mediagen.KindMusic || media.waitedFor != "" {
		t.Fatalf("result = %+v, requested %+v, waited %q", res, media.requested, media.waitedFor)
	}
	follow, ok := editor.sent(copilot.ScreenFollowJob)
	if !ok || !strings.Contains(renderJSON(follow.Args), `"purpose":"music"`) || !strings.Contains(renderJSON(follow.Args), `"atMs":2000`) {
		t.Fatalf("the editor must place the music at 2 s, got %s", renderJSON(follow.Args))
	}
	if err := tool.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"prompt": "x", "at_ms": -1.0}); err == nil {
		t.Fatal("a placement outside the video must be refused before the card")
	}
}

func TestStudioAskShowsAQuestionAndEndsTheTurn(t *testing.T) {
	res := NewStudioAskTool().Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{
		"question": "Para onde vai este vídeo?", "options": []interface{}{"Reels", "Feed"},
	})
	if res.Status != copilot.StatusOK || !res.EndTurn || res.Card == nil || res.Card.Kind != copilot.ActionAsk || len(res.Card.Question.Options) != 2 {
		t.Fatalf("result = %+v", res)
	}
	bad := NewStudioAskTool().Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{"question": "Ok?", "options": []interface{}{"Sim"}})
	if bad.Status != copilot.StatusError || bad.EndTurn {
		t.Fatalf("a question without real options must be refused, got %+v", bad)
	}
}

func TestStudioToolsFollowTheExecutionMode(t *testing.T) {
	deps := studioDeps(studio.KindVideo, &stubImages{}, &fakeJobs{})
	cases := map[string]struct {
		tool copilot.Tool
		asks map[copilot.Mode]bool
	}{
		"edit":     {NewStudioEditVideoTool(deps), map[copilot.Mode]bool{copilot.ModeAsk: true}},
		"job":      {NewStudioStartJobTool(deps), map[copilot.Mode]bool{copilot.ModeAsk: true}},
		"music":    {NewStudioGenerateMusicTool(deps), map[copilot.Mode]bool{copilot.ModeAsk: true, copilot.ModeEdit: true}},
		"image":    {NewStudioGenerateImageTool(deps), map[copilot.Mode]bool{copilot.ModeAsk: true, copilot.ModeEdit: true}},
		"read":     {NewStudioReadTool(deps), map[copilot.Mode]bool{}},
		"question": {NewStudioAskTool(), map[copilot.Mode]bool{}},
	}
	for name, tc := range cases {
		for _, mode := range []copilot.Mode{copilot.ModeAsk, copilot.ModeEdit, copilot.ModeFull} {
			if got := copilot.NeedsApproval(tc.tool, mode); got != tc.asks[mode] {
				t.Fatalf("%s in %s: needs approval = %v", name, mode, got)
			}
		}
	}
}

func TestAStudioImageAlwaysCarriesARecommendedModel(t *testing.T) {
	tool := NewStudioGenerateImageTool(studioDeps(studio.KindImage, &stubImages{}, &fakeJobs{}))
	choices, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), studioSession(copilot.StudioImage, &fakeEditor{}), nil)
	if err != nil || len(choices) != 1 || choices[0].Default == "" {
		t.Fatalf("full access needs a default model to run without a card, got %+v %v", choices, err)
	}
}

func TestAskModeShowsTheEditBatchOnTheCard(t *testing.T) {
	tool := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_text", "text": "Oi"},
		map[string]interface{}{"op": "animate", "clip_id": "c1", "property": "scale", "keys": []interface{}{}},
	}}
	cc := studioSession(copilot.StudioVideo, &fakeEditor{})
	if err := tool.(copilot.Validator).Validate(context.Background(), cc, args); err != nil {
		t.Fatal(err)
	}
	fields := renderJSON(tool.(copilot.Describer).Describe(context.Background(), cc, args))
	if !strings.Contains(fields, "add_text") || !strings.Contains(fields, "2") {
		t.Fatalf("the card must name the operations, got %s", fields)
	}
	if err := tool.(copilot.Validator).Validate(context.Background(), cc, map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "export"}}}); err == nil {
		t.Fatal("a malformed batch must not reach the card")
	}
}

func TestStudioImageEditCarriesEffectsMasksAndLayerOrder(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true, Data: []byte(`{"applied":3}`)}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_shape", "ref": "c", "shape": "ellipse", "gradient": map[string]interface{}{"kind": "radial", "from": "#ffffff", "to": "#000000", "cy": 0.4}},
		map[string]interface{}{"op": "order_layers", "layer_ids": []interface{}{"@c"}, "below_id": "photo"},
		map[string]interface{}{"op": "update_layer", "layer_id": "photo", "clip": true, "shadow": map[string]interface{}{"blur": 20.0}, "clear": []interface{}{"filters"}},
	}}
	res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"kind":"radial"`, `"cy":0.4`, `"below_id":"photo"`, `"clip":true`, `"blur":20`, `"clear":["filters"]`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	item := edit.Definition().Parameters["operations"].Items
	gradient := item.Properties["gradient"]
	if gradient.Type != "object" || gradient.Items == nil || len(gradient.Items.Properties["kind"].Enum) != 2 {
		t.Fatalf("gradient schema = %+v", gradient)
	}
	if clear := item.Properties["clear"]; clear.Items == nil || len(clear.Items.Enum) == 0 {
		t.Fatalf("clear schema = %+v", clear)
	}
}

func TestStudioLookPassesTheMarksChoiceToTheEditor(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenLook: {OK: true, Data: []byte(`{"marks":[{"n":1,"id":"l-1"}]}`), Images: []string{"data:image/jpeg;base64,AAAA"}}}}
	look := NewStudioLookTool(studioDeps(studio.KindImage, nil, nil))
	res := look.Execute(context.Background(), studioSession(copilot.StudioImage, editor), map[string]interface{}{"marks": true})
	if res.Status != copilot.StatusOK {
		t.Fatalf("look = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenLook)
	if sent := renderJSON(cmd.Args); !strings.Contains(sent, `"marks":true`) {
		t.Fatalf("marks were not sent: %s", sent)
	}
	if !strings.Contains(renderJSON(look.Definition()), "marks") {
		t.Fatal("the tool must offer marks")
	}
}
