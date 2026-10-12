package studiotools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/mediatools"
	"vozko/usecases/copilot/copilottools/tooltest"
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

func studioDeps(kind studio.Kind, media *tooltest.StubImages, jobs *fakeJobs) StudioDeps {
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

func TestASilentEditorTellsExactlyWhatHappenedAndNeverSendsTheModelToTheUser(t *testing.T) {
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_text", "text": "Oi"}}}
	cases := map[string]struct {
		err     error
		message string
	}{
		"nobody claimed the change": {copilot.ErrScreenUnavailable, studioEditorExpired},
		"claimed but unconfirmed":   {copilot.ErrScreenUnconfirmed, studioEditorUnconfirmed},
	}
	for name, tc := range cases {
		cc := studioSession(copilot.StudioImage, &fakeEditor{err: tc.err})
		res := edit.Execute(context.Background(), cc, args)
		if res.Status != copilot.StatusError || res.Message != tc.message {
			t.Fatalf("%s: result = %+v", name, res)
		}
		if res.AwaitsScreen == nil || *res.AwaitsScreen != cc.View {
			t.Fatalf("%s: the step must wait for this editor to answer again, got %+v", name, res.AwaitsScreen)
		}
	}
	read := NewStudioReadTool(studioDeps(studio.KindVideo, nil, nil))
	if res := read.Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{err: copilot.ErrScreenUnavailable}), nil); res.AwaitsScreen == nil {
		t.Fatalf("a silent read also waits for the editor, got %+v", res)
	}
	for _, message := range []string{studioEditorSilent, studioEditorExpired, studioEditorUnconfirmed} {
		if strings.Contains(message, "peça ao usuário para manter") || !strings.Contains(message, "Não peça ao usuário para avisar") || !strings.Contains(message, "o app avisa") {
			t.Fatalf("a silent editor is announced by the app, never by the user: %q", message)
		}
	}
	if !strings.Contains(studioEditorExpired, "nada deste pedido foi aplicado") || !strings.Contains(studioEditorExpired, "não entra depois") {
		t.Fatalf("an unclaimed change must say it never landed and never will: %q", studioEditorExpired)
	}
	absent := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {Error: &copilot.ScreenError{Code: copilot.ScreenNoEditor, Message: "não está aberto"}}}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, absent), args); res.AwaitsScreen != nil {
		t.Fatalf("a tab that answered is not a silent editor, got %+v", res)
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
		map[string]interface{}{"op": "update_clip", "clip_id": "c-2", "blur": 8.0},
		map[string]interface{}{"op": "animate", "clip_id": "c-3", "property": "blur", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 20.0}, map[string]interface{}{"at_ms": 500.0, "value": 0.0}}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"gradient":{`, `"highlight":{`, `"curve":0.4`, `"op":"add_icon"`, `"icon_id":"heart"`, `"blend_mode":"screen"`, `"clear":["gradient","highlight","curve","blend_mode"]`, `"blur":8`, `"property":"blur"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditPassesTransitions(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_transition", "ref": "t", "clip_id": "c-1", "transition_kind": "iris", "iris_shape": "diamond", "origin_x": 0.0, "duration_ms": 600.0, "alignment": "start", "easing": "cubic-bezier(0.2,0,0,1)"},
		map[string]interface{}{"op": "update_transition", "transition_id": "@t", "reverse": true},
		map[string]interface{}{"op": "remove_transition", "transition_id": "tr-9"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{"\"transition_kind\":\"iris\"", "\"iris_shape\":\"diamond\"", "\"origin_x\":0", "\"reverse\":true", "\"transition_id\":\"tr-9\""} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	bad := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_transition", "clip_id": "c-1", "transition_kind": "dissolve", "easing": "wiggle"}}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), bad); res.Status != copilot.StatusError {
		t.Fatalf("an unknown easing must be refused, got %+v", res)
	}
}

func TestStudioEditPassesTextAnimation(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "animate_text", "clip_id": "c-1", "side": "in", "text_style": "rise", "unit": "word", "stagger_ms": 0.0, "easing": "backOut"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"animate_text"`, `"side":"in"`, `"text_style":"rise"`, `"unit":"word"`, `"stagger_ms":0`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	fields := reflect.TypeOf(studioTextAnimationArgs{})
	styles, _ := fields.FieldByName("TextStyle")
	units, _ := fields.FieldByName("Unit")
	wantStyles := []string{}
	for _, style := range studio.TextStyles() {
		wantStyles = append(wantStyles, string(style))
	}
	wantUnits := []string{}
	for _, unit := range studio.TextUnits() {
		wantUnits = append(wantUnits, string(unit))
	}
	if styles.Tag.Get("enum") != strings.Join(append(wantStyles, "none"), ",") || units.Tag.Get("enum") != strings.Join(wantUnits, ",") {
		t.Fatalf("the schema drifted from the document: %q %q", styles.Tag.Get("enum"), units.Tag.Get("enum"))
	}
}

func TestStudioEditPassesSpeed(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_speed", "clip_id": "c-1", "speed": 0.5, "keep_duration": true, "reverse": true, "preserve_pitch": false, "stretch_keys": false, "ripple": false},
		map[string]interface{}{"op": "freeze_frame", "clip_id": "c-1", "at_ms": 1200.0, "duration_ms": 2000.0},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_speed"`, `"speed":0.5`, `"keep_duration":true`, `"reverse":true`, `"preserve_pitch":false`, `"stretch_keys":false`, `"ripple":false`, `"op":"freeze_frame"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditPassesRollAndSlide(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "roll_edit", "clip_id": "c-1", "edge": "end", "at_ms": 2600.0, "linked": false},
		map[string]interface{}{"op": "slide_clip", "clip_id": "c-2", "start_ms": 3100.0, "linked": true},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"roll_edit"`, `"edge":"end"`, `"at_ms":2600`, `"linked":false`, `"op":"slide_clip"`, `"start_ms":3100`, `"linked":true`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditPassesColorGrade(t *testing.T) {
	grade := map[string]interface{}{"exposure": 0.5, "temperature": -20.0, "saturation": 0.0}
	for _, tool := range []struct {
		kind   studio.Kind
		studio copilot.StudioKind
		edit   func(StudioDeps) copilot.Tool
		target map[string]interface{}
	}{
		{studio.KindVideo, copilot.StudioVideo, func(d StudioDeps) copilot.Tool { return NewStudioEditVideoTool(d) }, map[string]interface{}{"clip_id": "c-1"}},
		{studio.KindImage, copilot.StudioImage, func(d StudioDeps) copilot.Tool { return NewStudioEditImageTool(d) }, map[string]interface{}{"layer_id": "l-1"}},
	} {
		editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
		op := map[string]interface{}{"op": "set_color", "color_grade": grade, "look": "warm", "look_intensity": 0.5, "reset_color": true}
		for key, value := range tool.target {
			op[key] = value
		}
		res := tool.edit(studioDeps(tool.kind, nil, nil)).Execute(context.Background(), studioSession(tool.studio, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status != copilot.StatusOK {
			t.Fatalf("%s: result = %+v", tool.kind, res)
		}
		cmd, _ := editor.sent(copilot.ScreenEdit)
		sent := renderJSON(cmd.Args)
		for _, want := range []string{`"op":"set_color"`, `"exposure":0.5`, `"temperature":-20`, `"saturation":0`, `"look":"warm"`, `"look_intensity":0.5`, `"reset_color":true`} {
			if !strings.Contains(sent, want) {
				t.Fatalf("%s: the batch lost %s: %s", tool.kind, want, sent)
			}
		}
	}
}

func TestStudioEditPassesColorKeys(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	keys := []interface{}{
		map[string]interface{}{"at_ms": 0.0, "color": "#ff0000"},
		map[string]interface{}{"at_ms": 800.0, "color": "#0000ff", "easing": "spring"},
	}
	args := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "fill", "keys": keys}}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{"\"property\":\"fill\"", "\"color\":\"#0000ff\""} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	if strings.Contains(sent, "\"value\"") {
		t.Fatalf("a color key must not gain a value: %s", sent)
	}
}

func TestStudioEditPassesNamedAndCustomCurves(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	keys := []interface{}{
		map[string]interface{}{"at_ms": 0.0, "value": 0.2, "easing": "backOut"},
		map[string]interface{}{"at_ms": 500.0, "value": 0.5, "easing": "cubic-bezier(0.05,0.7,0.1,1)"},
		map[string]interface{}{"at_ms": 900.0, "value": 0.8},
	}
	args := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "x", "keys": keys}}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	if sent := renderJSON(cmd.Args); !strings.Contains(sent, "cubic-bezier(0.05,0.7,0.1,1)") || !strings.Contains(sent, "backOut") {
		t.Fatalf("the curves were lost: %s", sent)
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
		"unknown easing":        {"operations": []interface{}{map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "x", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.5, "easing": "wiggle"}}}}},
		"curve out of bounds":   {"operations": []interface{}{map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "x", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.5, "easing": "cubic-bezier(2,0,0,1)"}}}}},
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
	media := &tooltest.StubImages{}
	editor := startJobEditor()
	tool := NewStudioStartJobTool(studioDeps(studio.KindVideo, media, &fakeJobs{}))
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"kind": "captions", "clip_id": "c-1"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	if media.Requested.Kind != mediagen.KindCaptions || media.Requested.SourceMediaID != studioSourceID || media.RequestedBy != "u-1" {
		t.Fatalf("request = %+v by %s", media.Requested, media.RequestedBy)
	}
	if media.WaitedFor != "" {
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
		media  *tooltest.StubImages
		want   string
	}{
		"priced processing":          {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{priced: true}, startJobEditor(), &tooltest.StubImages{}, studioJobPriced},
		"no clip":                    {copilot.StudioVideo, map[string]interface{}{"kind": "denoise"}, &fakeJobs{}, startJobEditor(), &tooltest.StubImages{}, studioJobNeedsClip},
		"captions on an image":       {copilot.StudioImage, map[string]interface{}{"kind": "captions", "layer_id": "l-1"}, &fakeJobs{}, startJobEditor(), &tooltest.StubImages{}, studioJobNeedsLayer},
		"source that is not media":   {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{}, &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenResolveSource: {OK: true, Data: []byte(`{"sourceMediaId":"../x"}`)}}}, &tooltest.StubImages{}, studioSourceUnreadable},
		"too many processing jobs":   {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{}, startJobEditor(), &tooltest.StubImages{RequestErr: mediagen.ErrTooManyActive}, studioLaneFull(mediagen.KindCaptions)},
		"unknown processing request": {copilot.StudioVideo, map[string]interface{}{"kind": "captions", "clip_id": "c-1"}, &fakeJobs{}, startJobEditor(), &tooltest.StubImages{RequestErr: errors.New("db down")}, mediatools.MediaFailedUnknown},
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
	media := &tooltest.StubImages{}
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
	if res.Status != copilot.StatusOK || media.Requested.Kind != mediagen.KindMusic || media.WaitedFor != "" {
		t.Fatalf("result = %+v, requested %+v, waited %q", res, media.Requested, media.WaitedFor)
	}
	follow, ok := editor.sent(copilot.ScreenFollowJob)
	if !ok || !strings.Contains(renderJSON(follow.Args), `"purpose":"music"`) || !strings.Contains(renderJSON(follow.Args), `"atMs":2000`) {
		t.Fatalf("the editor must place the music at 2 s, got %s", renderJSON(follow.Args))
	}
	if err := tool.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"prompt": "x", "at_ms": -1.0}); err == nil {
		t.Fatal("a placement outside the video must be refused before the card")
	}
}

func askQuestion(text string, labels ...string) map[string]interface{} {
	options := make([]interface{}, 0, len(labels))
	for _, label := range labels {
		options = append(options, map[string]interface{}{"label": label})
	}
	return map[string]interface{}{"question": text, "options": options}
}

func TestStudioAskShowsAQuestionAndEndsTheTurn(t *testing.T) {
	res := NewStudioAskTool().Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{
		"questions": []interface{}{askQuestion("Para onde vai este vídeo?", "Reels", "Feed")},
	})
	if res.Status != copilot.StatusOK || !res.EndTurn || res.Card == nil || res.Card.Kind != copilot.ActionAsk || len(res.Card.Questions) != 1 || len(res.Card.Questions[0].Options) != 2 {
		t.Fatalf("result = %+v", res)
	}
	bad := NewStudioAskTool().Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{"questions": []interface{}{askQuestion("Ok?", "Sim")}})
	if bad.Status != copilot.StatusError || bad.EndTurn {
		t.Fatalf("a question without real options must be refused, got %+v", bad)
	}
}

func TestStudioAskCarriesAShortFormAnsweredTogether(t *testing.T) {
	format := askQuestion("Qual formato?", "Stories 9:16", "Feed 4:5")
	format["options"].([]interface{})[0].(map[string]interface{})["detail"] = "Tela cheia no celular"
	res := NewStudioAskTool().Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{
		"questions": []interface{}{format, askQuestion("Quanto tempo?", "6 s", "15 s", "30 s"), askQuestion("Qual o convite final?", "Inscreva-se", "Garanta sua vaga")},
	})
	if res.Status != copilot.StatusOK || !res.EndTurn || res.Card == nil || len(res.Card.Questions) != 3 {
		t.Fatalf("result = %+v", res)
	}
	if detail := res.Card.Questions[0].Options[0].Detail; detail != "Tela cheia no celular" {
		t.Fatalf("detail = %q", detail)
	}
	if data, _ := res.Data.(string); !strings.Contains(data, "juntas") {
		t.Fatalf("Elo must learn the answers arrive together, got %v", res.Data)
	}
	five := make([]interface{}, 0, copilot.MaxFormQuestions+1)
	for i := 0; i <= copilot.MaxFormQuestions; i++ {
		five = append(five, askQuestion(fmt.Sprintf("Pergunta %d?", i), "Sim", "Não"))
	}
	if long := NewStudioAskTool().Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{"questions": five}); long.Status != copilot.StatusError || long.EndTurn {
		t.Fatalf("a form longer than the card must be refused, got %+v", long)
	}
}

func TestStudioAskDescribesEachQuestionAndOption(t *testing.T) {
	def := NewStudioAskTool().Definition()
	questions := def.Parameters["questions"]
	if questions.Type != "array" || questions.Items == nil || questions.Items.Type != "object" || !copilottools.Contains(def.Required, "questions") {
		t.Fatalf("questions = %+v", questions)
	}
	options := questions.Items.Properties["options"]
	if questions.Items.Properties["question"].Type != "string" || options.Type != "array" || options.Items == nil {
		t.Fatalf("question items = %+v", questions.Items.Properties)
	}
	if options.Items.Properties["label"].Type != "string" || options.Items.Properties["detail"].Type != "string" || !copilottools.Contains(options.Items.Required, "label") {
		t.Fatalf("option items = %+v", options.Items)
	}
}

func TestStudioToolsFollowTheExecutionMode(t *testing.T) {
	deps := studioDeps(studio.KindVideo, &tooltest.StubImages{}, &fakeJobs{})
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
	tool := NewStudioGenerateImageTool(studioDeps(studio.KindImage, &tooltest.StubImages{}, &fakeJobs{}))
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

func TestStudioExportRunsOnTheEditor(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenExport: {OK: true, Data: json.RawMessage(`{"saved":[{"media_id":"m-1"}]}`)}}}
	export := NewStudioExportTool(studioDeps(studio.KindImage, nil, nil))
	res := export.Execute(context.Background(), studioSession(copilot.StudioImage, editor), map[string]interface{}{"format": "jpeg", "scale": 2.0, "artboards": "all", "quality": 0.8})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, ok := editor.sent(copilot.ScreenExport)
	if !ok {
		t.Fatal("no export command reached the editor")
	}
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"format":"jpeg"`, `"scale":2`, `"artboards":"all"`, `"quality":0.8`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the export lost %s: %s", want, sent)
		}
	}
	bad := export.Execute(context.Background(), studioSession(copilot.StudioImage, &fakeEditor{}), map[string]interface{}{"scale": 5.0})
	if bad.Status != copilot.StatusError {
		t.Fatalf("a scale out of range must be refused, got %+v", bad)
	}
}

func TestStudioExportSendsAnimationAndVectorFormatsToTheirEditors(t *testing.T) {
	video := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenExport: {OK: true, Data: json.RawMessage(`{"started":true}`)}}}
	gif := NewStudioExportTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, video), map[string]interface{}{"format": "gif", "width": 480.0, "fps": 15.0, "colors": 128.0, "dither": "ordered", "loop": 0.0})
	if gif.Status != copilot.StatusOK {
		t.Fatalf("gif = %+v", gif)
	}
	cmd, _ := video.sent(copilot.ScreenExport)
	for _, want := range []string{`"format":"gif"`, `"width":480`, `"fps":15`, `"colors":128`, `"dither":"ordered"`, `"loop":0`} {
		if sent := renderJSON(cmd.Args); !strings.Contains(sent, want) {
			t.Fatalf("the export lost %s: %s", want, sent)
		}
	}
	image := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenExport: {OK: true, Data: json.RawMessage(`{"saved":[]}`)}}}
	pdf := NewStudioExportTool(studioDeps(studio.KindImage, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioImage, image), map[string]interface{}{"format": "pdf", "preset": "print", "artboards": "all"})
	if pdf.Status != copilot.StatusOK {
		t.Fatalf("pdf = %+v", pdf)
	}
	sentPDF, _ := image.sent(copilot.ScreenExport)
	if sent := renderJSON(sentPDF.Args); !strings.Contains(sent, `"preset":"print"`) {
		t.Fatalf("the preset was lost: %s", sent)
	}
}

func TestStudioExportSendsARangeOrAFrameToTheVideoEditor(t *testing.T) {
	cases := map[string]struct {
		args map[string]interface{}
		want []string
	}{
		"mp4 range":      {map[string]interface{}{"from_ms": 2000.0, "to_ms": 6000.0}, []string{`"from_ms":2000`, `"to_ms":6000`}},
		"gif from start": {map[string]interface{}{"format": "gif", "from_ms": 0.0, "to_ms": 1500.0}, []string{`"format":"gif"`, `"from_ms":0`, `"to_ms":1500`}},
		"png frame":      {map[string]interface{}{"format": "png", "at_ms": 1200.0}, []string{`"format":"png"`, `"at_ms":1200`}},
		"jpeg frame":     {map[string]interface{}{"format": "jpeg", "at_ms": 0.0, "quality": 0.7}, []string{`"format":"jpeg"`, `"at_ms":0`, `"quality":0.7`}},
	}
	for name, c := range cases {
		editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenExport: {OK: true, Data: json.RawMessage(`{"started":true}`)}}}
		res := NewStudioExportTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, editor), c.args)
		if res.Status != copilot.StatusOK {
			t.Fatalf("%s: %+v", name, res)
		}
		cmd, _ := editor.sent(copilot.ScreenExport)
		sent := renderJSON(cmd.Args)
		for _, want := range c.want {
			if !strings.Contains(sent, want) {
				t.Fatalf("%s lost %s: %s", name, want, sent)
			}
		}
	}
}

func TestStudioExportDescribesRangesAndFrames(t *testing.T) {
	described := renderJSON(NewStudioExportTool(studioDeps(studio.KindVideo, nil, nil)).Definition())
	for _, want := range []string{"from_ms", "to_ms", "at_ms", "work_area"} {
		if !strings.Contains(described, want) {
			t.Fatalf("the tool must explain %s: %s", want, described)
		}
	}
}

func TestStudioExportRefusesWhatTheProjectCannotExport(t *testing.T) {
	cases := map[string]struct {
		kind    studio.Kind
		session copilot.StudioKind
		args    map[string]interface{}
	}{
		"png from a video without a moment": {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "png"}},
		"negative moment":                   {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "jpeg", "at_ms": -1.0}},
		"moment on mp4":                     {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "mp4", "at_ms": 100.0}},
		"moment on an image":                {studio.KindImage, copilot.StudioImage, map[string]interface{}{"format": "png", "at_ms": 100.0}},
		"range on a frame":                  {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "png", "at_ms": 100.0, "from_ms": 0.0}},
		"range on an image":                 {studio.KindImage, copilot.StudioImage, map[string]interface{}{"format": "pdf", "to_ms": 100.0}},
		"range that ends before it starts":  {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "from_ms": 3000.0, "to_ms": 1000.0}},
		"range that starts before zero":     {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"from_ms": -5.0}},
		"empty range":                       {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"from_ms": 0.0, "to_ms": 0.0}},
		"scale on a video frame":            {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "png", "at_ms": 0.0, "scale": 2.0}},
		"artboards on a video frame":        {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "jpeg", "at_ms": 0.0, "artboards": "all"}},
		"gif from an image":                 {studio.KindImage, copilot.StudioImage, map[string]interface{}{"format": "gif"}},
		"scale on a video":                  {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "mp4", "scale": 2.0}},
		"gif options on mp4":                {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "mp4", "fps": 15.0}},
		"preset on png":                     {studio.KindImage, copilot.StudioImage, map[string]interface{}{"format": "png", "preset": "print"}},
		"colors on webp":                    {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "webp", "colors": 64.0}},
		"quality on gif":                    {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "quality": 0.8}},
		"narrow gif":                        {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "width": 50.0}},
		"wide gif":                          {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "width": 4000.0}},
		"fast gif":                          {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "fps": 60.0}},
		"one colour":                        {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "colors": 1.0}},
		"negative loop":                     {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "loop": -1.0}},
		"endless loop count":                {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "webp", "loop": 70000.0}},
		"unknown dither":                    {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "dither": "noise"}},
		"unknown format":                    {studio.KindImage, copilot.StudioImage, map[string]interface{}{"format": "tiff"}},
		"artboards on a video":              {studio.KindVideo, copilot.StudioVideo, map[string]interface{}{"format": "gif", "artboards": "all"}},
	}
	for name, c := range cases {
		editor := &fakeEditor{}
		res := NewStudioExportTool(studioDeps(c.kind, nil, nil)).Execute(context.Background(), studioSession(c.session, editor), c.args)
		if res.Status != copilot.StatusError {
			t.Fatalf("%s: got %+v", name, res)
		}
		if _, sent := editor.sent(copilot.ScreenExport); sent {
			t.Fatalf("%s: the editor received a refused export", name)
		}
	}
}

func TestStudioEditPassesSoundLevels(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "update_track", "track_id": "t-1", "gain_db": -3.0},
		map[string]interface{}{"op": "set_ducking", "track_id": "t-1", "trigger_track_ids": []interface{}{"t-2"}, "reduce_db": 15.0, "attack_ms": 200.0, "release_ms": 600.0, "hold_ms": 250.0, "sensitivity": 0.4},
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "gainDb", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": -6.0}}},
		map[string]interface{}{"op": "bake_ducking", "track_id": "t-1"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"gain_db":-3`, `"op":"set_ducking"`, `"trigger_track_ids":["t-2"]`, `"reduce_db":15`, `"sensitivity":0.4`, `"property":"gainDb"`, `"op":"bake_ducking"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditPassesPanCrossfadesAndLoudness(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "update_clip", "clip_id": "c-1", "pan": -0.5},
		map[string]interface{}{"op": "animate", "clip_id": "c-2", "property": "pan", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": -1.0}, map[string]interface{}{"at_ms": 900.0, "value": 1.0}}},
		map[string]interface{}{"op": "update_track", "track_id": "t-1", "pan": 0.25},
		map[string]interface{}{"op": "add_transition", "clip_id": "c-1", "transition_kind": "constantGain", "duration_ms": 1000.0},
		map[string]interface{}{"op": "normalize_loudness", "clip_ids": []interface{}{"c-1", "c-2"}, "target_lufs": -16.0},
		map[string]interface{}{"op": "normalize_loudness", "track_id": "t-1", "target_lufs": -23.0},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"pan":-0.5`, `"property":"pan"`, `"pan":0.25`, `"transition_kind":"constantGain"`, `"op":"normalize_loudness"`, `"target_lufs":-16`, `"target_lufs":-23`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	item := edit.Definition().Parameters["operations"].Items
	if !copilottools.Contains(item.Properties["transition_kind"].Enum, "constantPower") || !copilottools.Contains(item.Properties["transition_kind"].Enum, "constantGain") || !copilottools.Contains(item.Properties["property"].Enum, "pan") {
		t.Fatalf("the schema does not offer the audio kinds and pan: %+v", item.Properties["transition_kind"].Enum)
	}
	if item.Properties["pan"].Type != "number" || item.Properties["target_lufs"].Type != "number" {
		t.Fatalf("pan and target_lufs must be numbers: %+v %+v", item.Properties["pan"], item.Properties["target_lufs"])
	}
	if listen := NewStudioListenTool(studioDeps(studio.KindVideo, nil, nil)).Definition(); !strings.Contains(listen.Description, "balance_db") || !strings.Contains(listen.Description, "pan") {
		t.Fatalf("studio_listen does not explain the stereo balance: %s", listen.Description)
	}
}

func TestStudioEditPassesMasks(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_mask", "ref": "m", "clip_id": "c-1", "mask_shape": "star", "mask_mode": "subtract", "invert": true, "points": 6.0, "inner": 0.4, "w": 0.5, "feather": 0.1, "expansion": -0.05, "roundness": 0.0},
		map[string]interface{}{"op": "update_mask", "clip_id": "c-1", "mask_id": "@m", "x": 0.3, "at_ms": 400.0},
		map[string]interface{}{"op": "animate_mask", "clip_id": "c-1", "mask_id": "@m", "mask_property": "opacity", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.0}}},
		map[string]interface{}{"op": "move_mask", "clip_id": "c-1", "mask_id": "@m", "to_index": 0.0},
		map[string]interface{}{"op": "remove_mask", "clip_id": "c-1", "mask_id": "@m"},
		map[string]interface{}{"op": "update_clip", "clip_id": "c-2", "matte": true},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"add_mask"`, `"mask_shape":"star"`, `"mask_mode":"subtract"`, `"invert":true`, `"points":6`, `"feather":0.1`, `"expansion":-0.05`, `"roundness":0`, `"mask_id":"@m"`, `"mask_property":"opacity"`, `"op":"move_mask"`, `"op":"remove_mask"`, `"matte":true`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditPassesChromaKey(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_chroma_key", "clip_id": "c-1", "chroma_preset": "blue", "chroma_color": "#00ff00", "similarity": 0.35, "smoothness": 0.1, "spill": 0.2, "at_ms": 0.0},
		map[string]interface{}{"op": "animate_chroma", "clip_id": "c-1", "chroma_property": "spill", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.2}}},
		map[string]interface{}{"op": "remove_chroma_key", "clip_id": "c-1"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_chroma_key"`, `"chroma_preset":"blue"`, `"chroma_color":"#00ff00"`, `"similarity":0.35`, `"smoothness":0.1`, `"spill":0.2`, `"chroma_property":"spill"`, `"op":"remove_chroma_key"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditImagePassesChromaKey(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_chroma_key", "layer_id": "photo", "chroma_preset": "auto", "similarity": 0.3, "smoothness": 0.1, "spill": 0.2},
		map[string]interface{}{"op": "set_chroma_key", "layer_ids": []interface{}{"photo"}, "chroma_color": "#20c040"},
		map[string]interface{}{"op": "remove_chroma_key", "layer_id": "photo"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_chroma_key"`, `"chroma_preset":"auto"`, `"chroma_color":"#20c040"`, `"similarity":0.3`, `"smoothness":0.1`, `"spill":0.2`, `"op":"remove_chroma_key"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	item := edit.Definition().Parameters["operations"].Items
	if _, ok := item.Properties["chroma_property"]; ok {
		t.Fatal("the image tool offers chroma_property, but still images have no keys")
	}
	if preset := item.Properties["chroma_preset"].Enum; !copilottools.Contains(preset, "auto") || !copilottools.Contains(preset, "green") || !copilottools.Contains(preset, "blue") {
		t.Fatalf("chroma_preset enum = %v", preset)
	}
	video := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items
	if !copilottools.Contains(video.Properties["chroma_preset"].Enum, "auto") {
		t.Fatal("the video tool cannot ask for the detected key color")
	}
}
