package studiotools

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools/tooltest"
	"vozko/usecases/copilot/skills"
)

const studioStyleID = "3c4d5e6f-7a8b-4c9d-8e0f-1a2b3c4d5e6f"

func TestStudioStartJobRemovesTheBackgroundOfWhatAnImageElementShows(t *testing.T) {
	media := &tooltest.StubImages{}
	editor := startJobEditor()
	tool := NewStudioStartJobTool(studioDeps(studio.KindImage, media, &fakeJobs{}))
	res := tool.Execute(context.Background(), studioSession(copilot.StudioImage, editor), map[string]interface{}{"kind": "cutout", "layer_id": "moldura-1"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	resolve, _ := editor.sent(copilot.ScreenResolveSource)
	if sent := renderJSON(resolve.Args); !strings.Contains(sent, `"layerId":"moldura-1"`) || !strings.Contains(sent, `"kind":"cutout"`) {
		t.Fatalf("the editor must resolve the picture the element shows, got %s", sent)
	}
	if media.Requested.Kind != mediagen.KindCutout || media.Requested.SourceMediaID != studioSourceID {
		t.Fatalf("request = %+v", media.Requested)
	}
	follow, ok := editor.sent(copilot.ScreenFollowJob)
	if sent := renderJSON(follow.Args); !ok || !strings.Contains(sent, `"layerId":"moldura-1"`) || !strings.Contains(sent, `"sourceMediaId":"`+studioSourceID+`"`) {
		t.Fatalf("the editor must follow the job with what it started from, got %s", sent)
	}
}

func imageEditArgs(extra map[string]interface{}) map[string]interface{} {
	args := tooltest.ImageArgs()
	for key, value := range extra {
		args[key] = value
	}
	return args
}

func TestStudioImageEditSendsWhatTheElementShowsFirstAndPutsTheResultBack(t *testing.T) {
	media := &tooltest.StubImages{}
	editor := startJobEditor()
	tool := NewStudioGenerateImageTool(studioDeps(studio.KindImage, media, &fakeJobs{}))
	args := imageEditArgs(map[string]interface{}{"layer_id": "celula-2", "reference_media_ids": []interface{}{studioStyleID, studioSourceID}})
	res := tool.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	resolve, _ := editor.sent(copilot.ScreenResolveSource)
	if sent := renderJSON(resolve.Args); !strings.Contains(sent, `"layerId":"celula-2"`) || !strings.Contains(sent, `"kind":"image"`) {
		t.Fatalf("the editor must resolve the picture the element shows, got %s", sent)
	}
	if want := []string{studioSourceID, studioStyleID}; !reflect.DeepEqual(media.Requested.ReferenceMediaIDs, want) {
		t.Fatalf("the shown picture must lead the references once, got %v", media.Requested.ReferenceMediaIDs)
	}
	follow, ok := editor.sent(copilot.ScreenFollowJob)
	sent := renderJSON(follow.Args)
	for _, want := range []string{`"purpose":"image"`, `"layerId":"celula-2"`, `"referenceMediaIds":["` + studioSourceID} {
		if !ok || !strings.Contains(sent, want) {
			t.Fatalf("missing %s in the follow %s", want, sent)
		}
	}
}

func TestStudioImageEditTargetsOnlyImageProjectsAndKeepsRoomForThePicture(t *testing.T) {
	tooMany := make([]interface{}, 0, mediagen.MaxReferenceImages)
	for i := 0; i < mediagen.MaxReferenceImages; i++ {
		tooMany = append(tooMany, fmt.Sprintf("%s%02x", studioStyleID[:34], i))
	}
	cases := map[string]struct {
		kind   copilot.StudioKind
		studio studio.Kind
		args   map[string]interface{}
		want   string
	}{
		"a video project": {copilot.StudioVideo, studio.KindVideo, imageEditArgs(map[string]interface{}{"layer_id": "l-1"}), studioEditNeedsImage},
		"no room":         {copilot.StudioImage, studio.KindImage, imageEditArgs(map[string]interface{}{"layer_id": "l-1", "reference_media_ids": tooMany}), studioEditNoRoom},
	}
	for name, tc := range cases {
		editor := startJobEditor()
		media := &tooltest.StubImages{}
		tool := NewStudioGenerateImageTool(studioDeps(tc.studio, media, &fakeJobs{}))
		cc := studioSession(tc.kind, editor)
		if err := tool.(copilot.Validator).Validate(context.Background(), cc, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: validate = %v", name, err)
		}
		if res := tool.Execute(context.Background(), cc, tc.args); res.Status != copilot.StatusError || res.Message != tc.want {
			t.Fatalf("%s: result = %+v", name, res)
		}
		if len(editor.commands) > 0 || media.Requested.Kind != "" {
			t.Fatalf("%s: nothing may reach the editor or the queue", name)
		}
	}
}

func TestStudioImageEditStopsWhenTheEditorFindsNoPicture(t *testing.T) {
	media := &tooltest.StubImages{}
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{
		copilot.ScreenResolveSource: {OK: false, Error: &copilot.ScreenError{Code: "wrong_source", Message: "o elemento precisa mostrar uma única imagem"}},
	}}
	tool := NewStudioGenerateImageTool(studioDeps(studio.KindImage, media, &fakeJobs{}))
	res := tool.Execute(context.Background(), studioSession(copilot.StudioImage, editor), imageEditArgs(map[string]interface{}{"layer_id": "texto"}))
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "única imagem") {
		t.Fatalf("result = %+v", res)
	}
	if _, followed := editor.sent(copilot.ScreenFollowJob); followed || media.Requested.Kind != "" {
		t.Fatal("nothing may be queued or followed without the picture")
	}
}

func TestTheImageSkillTeachesJobsOnWhatAnElementShows(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, ok := library.Find("design-de-imagem")
	if !ok {
		t.Fatal("the design-de-imagem skill is missing")
	}
	for _, want := range []string{"studio_start_job kind cutout", "studio_generate_image com layer_id", "moldura", "mockup", "grade", "preenchimento de imagem", "shows"} {
		if !strings.Contains(skill.Body, want) {
			t.Errorf("design-de-imagem must teach %q", want)
		}
	}
}

func TestOnlyTheImageStudioOffersEditingWhatAnElementShows(t *testing.T) {
	tool := NewStudioGenerateImageTool(StudioDeps{})
	image := copilot.DefinitionOn(tool, copilot.View{Surface: copilot.SurfaceStudio, ProjectID: studioProjectID, ProjectKind: copilot.StudioImage})
	video := copilot.DefinitionOn(tool, copilot.View{Surface: copilot.SurfaceStudio, ProjectID: studioProjectID, ProjectKind: copilot.StudioVideo})
	if image.Parameters["layer_id"].Type != "string" {
		t.Fatalf("the image studio must offer layer_id, got %+v", image.Parameters)
	}
	if _, ok := video.Parameters["layer_id"]; ok || !reflect.DeepEqual(video, tool.Definition()) {
		t.Fatal("the video studio keeps its definition untouched")
	}
}
