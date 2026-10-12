package studiotools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func TestASoundEffectIsGeneratedForTheVideoAndPlacedAtTheAskedTime(t *testing.T) {
	media := &tooltest.StubImages{}
	editor := &fakeEditor{}
	tool := NewStudioGenerateSoundEffectTool(studioDeps(studio.KindVideo, media, &fakeJobs{}))
	def := tool.Definition()
	if def.Name != "studio_generate_sfx" || def.Parameters["prompt"].Type == "" || def.Parameters["length_ms"].Type == "" || def.Parameters["at_ms"].Type == "" || def.Parameters["track_id"].Type == "" {
		t.Fatalf("definition = %+v", def)
	}
	if !copilottools.Contains(def.Required, "prompt") || !copilottools.Contains(def.Required, "length_ms") {
		t.Fatalf("required = %v", def.Required)
	}
	choices, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), studioSession(copilot.StudioVideo, editor), nil)
	if err != nil || len(choices) != 1 || choices[0].Kind != copilot.ChoiceSoundEffectModel || choices[0].Key != "sfx_model" || choices[0].Default == "" {
		t.Fatalf("choices = %+v, %v", choices, err)
	}
	args := map[string]interface{}{"prompt": "whoosh rápido de transição", "length_ms": 1200.0, "at_ms": 4000.0, "track_id": "a-2", "sfx_model": "bytedance-seed/seed-audio-1-0"}
	if err := tool.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"prompt": "whoosh", "length_ms": 1200.0, "at_ms": 4000.0}); err != nil {
		t.Fatal(err)
	}
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args)
	got := media.Requested
	if res.Status != copilot.StatusOK || got.Kind != mediagen.KindSoundEffect || got.DurationMS != 1200 || got.Prompt != "whoosh rápido de transição" || got.Model != "bytedance-seed/seed-audio-1-0" || media.WaitedFor != "" {
		t.Fatalf("result = %+v, requested %+v, waited %q", res, media.Requested, media.WaitedFor)
	}
	follow, ok := editor.sent(copilot.ScreenFollowJob)
	rendered := renderJSON(follow.Args)
	if !ok || !strings.Contains(rendered, `"purpose":"sfx"`) || !strings.Contains(rendered, `"atMs":4000`) || !strings.Contains(rendered, `"trackId":"a-2"`) {
		t.Fatalf("the editor must place the effect at 4 s on track a-2, got %s", rendered)
	}
}

func TestASoundEffectOutsideItsLengthIsRefusedBeforeTheCard(t *testing.T) {
	tool := NewStudioGenerateSoundEffectTool(studioDeps(studio.KindVideo, &tooltest.StubImages{}, &fakeJobs{}))
	session := studioSession(copilot.StudioVideo, &fakeEditor{})
	for _, length := range []float64{0, float64(mediagen.MinSoundEffectMS - 1), float64(mediagen.MaxSoundEffectMS + 1)} {
		if err := tool.(copilot.Validator).Validate(context.Background(), session, map[string]interface{}{"prompt": "impacto", "length_ms": length}); err == nil {
			t.Fatalf("a %v ms effect passed", length)
		}
	}
}

func TestASoundEffectIsOfferedOnlyInVideoProjectsAndDescribedOnTheCard(t *testing.T) {
	tool := NewStudioGenerateSoundEffectTool(studioDeps(studio.KindVideo, &tooltest.StubImages{}, &fakeJobs{}))
	video := copilot.View{Surface: copilot.SurfaceStudio, ProjectKind: copilot.StudioVideo}
	image := copilot.View{Surface: copilot.SurfaceStudio, ProjectKind: copilot.StudioImage}
	if !video.Offers(tool) || image.Offers(tool) {
		t.Fatal("sound effects belong to video projects")
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{"prompt": "riser", "length_ms": 2500.0, "at_ms": 1000.0})
	got := map[string]string{}
	for _, field := range fields {
		got[field.Key] = field.Value
	}
	if got["effect"] != "riser" || got["length"] != "2.5 s" || got["at"] != "1 s" || got["cost"] == "" {
		t.Fatalf("fields = %+v", fields)
	}
	found := false
	for _, registered := range StudioTools(StudioDeps{}) {
		found = found || registered.Definition().Name == "studio_generate_sfx"
	}
	if !found {
		t.Fatal("the studio offers the sound effect tool")
	}
}

func TestTheSoundEffectToolNamesNoVendor(t *testing.T) {
	tool := NewStudioGenerateSoundEffectTool(studioDeps(studio.KindVideo, &tooltest.StubImages{}, &fakeJobs{}))
	def := tool.Definition()
	text := strings.ToLower(def.Description)
	for _, param := range def.Parameters {
		text += " " + strings.ToLower(param.Description)
	}
	for _, vendor := range []string{"seed", "bytedance", "openrouter", "elevenlabs", "lyria", "google", "openai"} {
		if strings.Contains(text, vendor) {
			t.Fatalf("the tool text names %q: %s", vendor, text)
		}
	}
}

func TestAFullGenerationLaneTellsEloHowToRecover(t *testing.T) {
	media := &tooltest.StubImages{RequestErr: mediagen.ErrTooManyActive}
	editor := &fakeEditor{}
	tool := NewStudioGenerateSoundEffectTool(studioDeps(studio.KindVideo, media, &fakeJobs{}))
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"prompt": "impacto grave", "length_ms": 1200.0, "sfx_model": "bytedance-seed/seed-audio-1-0"})
	if res.Status != copilot.StatusError {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{fmt.Sprintf("%d gerações", mediagen.MaxActiveGenerations), "efeito sonoro", "nada foi gerado nem cobrado", "studio_jobs", "wait_seconds", "um por vez"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("the refusal must say %q, got %q", want, res.Message)
		}
	}
	if _, followed := editor.sent(copilot.ScreenFollowJob); followed {
		t.Fatal("the editor must not follow a job that was not queued")
	}
}

func TestAFullProcessingLaneNamesItsOwnLimit(t *testing.T) {
	media := &tooltest.StubImages{RequestErr: mediagen.ErrTooManyActive}
	tool := NewStudioStartJobTool(studioDeps(studio.KindVideo, media, &fakeJobs{}))
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, startJobEditor()), map[string]interface{}{"kind": "captions", "clip_id": "c-1"})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, fmt.Sprintf("%d processamentos", mediagen.MaxActiveProcessing)) || !strings.Contains(res.Message, "studio_jobs") || strings.Contains(res.Message, "efeito sonoro") {
		t.Fatalf("result = %+v", res)
	}
}
