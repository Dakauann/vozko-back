package studiotools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func lookEditor() *fakeEditor {
	return &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenLook: {OK: true, Data: []byte(`{"frames":[]}`), Images: []string{"data:image/jpeg;base64,AAAA"}}}}
}

func TestStudioLookSendsMotionWindowsAndZoomToTheEditor(t *testing.T) {
	editor := lookEditor()
	look := NewStudioLookTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"focus_id": "t-1", "count": 7.0, "region": map[string]interface{}{"x": 0.5, "y": 0.85, "w": 1.0, "h": 0.3}}
	if res := look.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("look = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenLook)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"focus_id":"t-1"`, `"count":7`, `"region":{"x":0.5,"y":0.85,"w":1,"h":0.3}`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("look args %s miss %s", sent, want)
		}
	}
	windowed := lookEditor()
	if res := look.Execute(context.Background(), studioSession(copilot.StudioVideo, windowed), map[string]interface{}{"from_ms": 1000.0, "to_ms": 1600.0}); res.Status != copilot.StatusOK {
		t.Fatalf("window look = %+v", res)
	}
	cmd, _ = windowed.sent(copilot.ScreenLook)
	if sent := renderJSON(cmd.Args); !strings.Contains(sent, `"from_ms":1000`) || !strings.Contains(sent, `"to_ms":1600`) {
		t.Fatalf("window args %s", sent)
	}
	def := renderJSON(look.Definition())
	for _, want := range []string{"focus_id", "from_ms", "to_ms", "region"} {
		if !strings.Contains(def, want) {
			t.Fatalf("the look schema must offer %s", want)
		}
	}
}

func TestStudioLookNamesItsTargetForTheChatWithoutTellingTheModel(t *testing.T) {
	from, to := int64(3000), int64(5000)
	editor := lookEditor()
	reply := editor.replies[copilot.ScreenLook]
	reply.Target = &copilot.ScreenTarget{Kind: copilot.LookMoments, FromMS: &from, ToMS: &to}
	editor.replies[copilot.ScreenLook] = reply
	res := NewStudioLookTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{})
	if res.Status != copilot.StatusOK || res.Subject == nil || res.Subject.Target == nil {
		t.Fatalf("look = %+v", res)
	}
	if res.Subject.Kind != copilot.SubjectStudioLook || res.Subject.Key != copilot.LookMoments || res.Subject.Target.ProjectID != studioProjectID || *res.Subject.Target.FromMS != from || *res.Subject.Target.ToMS != to {
		t.Fatalf("subject = %+v target = %+v", res.Subject, res.Subject.Target)
	}
	if renderJSON(res.Data) != `{"frames":[]}` {
		t.Fatalf("the model must receive the editor data unchanged, got %s", renderJSON(res.Data))
	}

	named := lookEditor()
	board := named.replies[copilot.ScreenLook]
	board.Target = &copilot.ScreenTarget{Kind: copilot.LookArtboard, ArtboardID: "ab-2", N: 2, Name: "Capa"}
	named.replies[copilot.ScreenLook] = board
	res = NewStudioLookTool(studioDeps(studio.KindImage, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioImage, named), map[string]interface{}{})
	if res.Subject == nil || res.Subject.Label != "Capa" || res.Subject.Target.ArtboardID != "ab-2" || res.Subject.Target.ProjectID != studioProjectID {
		t.Fatalf("artboard subject = %+v", res.Subject)
	}

	silent := lookEditor()
	if res := NewStudioLookTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, silent), map[string]interface{}{}); res.Subject != nil {
		t.Fatalf("an editor that names nothing leaves the step unnamed, got %+v", res.Subject)
	}
}

func TestStudioLookRefusesAmbiguousOrMalformedLooks(t *testing.T) {
	look := NewStudioLookTool(studioDeps(studio.KindVideo, nil, nil))
	bad := []map[string]interface{}{
		{"from_ms": 2000.0, "to_ms": 1000.0},
		{"to_ms": 1000.0},
		{"focus_id": "t-1", "times_ms": []interface{}{100.0}},
		{"focus_id": "t-1", "from_ms": 0.0, "to_ms": 500.0},
		{"region": map[string]interface{}{"x": 0.5, "y": 0.5, "w": 0.0, "h": 0.2}},
		{"region": map[string]interface{}{"x": 1.5, "y": 0.5, "w": 0.2, "h": 0.2}},
		{"media_id": "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", "region": map[string]interface{}{"x": 0.5, "y": 0.5, "w": 0.2, "h": 0.2}},
		{"media_id": "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", "focus_id": "t-1"},
	}
	for _, args := range bad {
		editor := lookEditor()
		if res := look.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusError {
			t.Fatalf("%v must be refused, got %+v", args, res)
		}
		if _, sent := editor.sent(copilot.ScreenLook); sent {
			t.Fatalf("%v reached the editor", args)
		}
	}
}

func listenEditor() *fakeEditor {
	return &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenListen: {OK: true, Data: json.RawMessage(`{"summary":{"integrated_lufs":-14.2}}`)}}}
}

func TestStudioListenReadsTheMixOnTheEditor(t *testing.T) {
	editor := listenEditor()
	listen := NewStudioListenTool(studioDeps(studio.KindVideo, nil, nil))
	cc := studioSession(copilot.StudioVideo, editor)
	cc.SeesImages = false
	args := map[string]interface{}{"from_ms": 1000.0, "to_ms": 9000.0, "window_ms": 500.0, "voice_track_ids": []interface{}{"t-voz"}}
	res := listen.Execute(context.Background(), cc, args)
	if res.Status != copilot.StatusOK || !strings.Contains(renderJSON(res.Data), "integrated_lufs") || len(res.Images) != 0 {
		t.Fatalf("listen = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenListen)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"from_ms":1000`, `"to_ms":9000`, `"window_ms":500`, `"voice_track_ids":["t-voz"]`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("listen args %s miss %s", sent, want)
		}
	}
}

func TestStudioListenRefusesBadRangesBeforeTheEditor(t *testing.T) {
	listen := NewStudioListenTool(studioDeps(studio.KindVideo, nil, nil))
	for _, args := range []map[string]interface{}{
		{"from_ms": 5000.0, "to_ms": 1000.0},
		{"window_ms": 100.0},
		{"window_ms": 20000.0},
		{"to_ms": float64(studio.MaxVideoMS + 1)},
		{"from_ms": -1.0},
	} {
		editor := listenEditor()
		if res := listen.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusError {
			t.Fatalf("%v must be refused, got %+v", args, res)
		}
		if _, sent := editor.sent(copilot.ScreenListen); sent {
			t.Fatalf("%v reached the editor", args)
		}
	}
}

func TestStudioListenIsOfferedOnlyOnVideoProjects(t *testing.T) {
	listen := NewStudioListenTool(StudioDeps{})
	video := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioVideo}
	image := video
	image.ProjectKind = copilot.StudioImage
	if !video.Offers(listen) || image.Offers(listen) {
		t.Fatal("studio_listen belongs to video projects only")
	}
	found := false
	for _, tool := range StudioTools(StudioDeps{}) {
		found = found || tool.Definition().Name == "studio_listen"
	}
	if !found {
		t.Fatal("studio_listen is not registered")
	}
}
