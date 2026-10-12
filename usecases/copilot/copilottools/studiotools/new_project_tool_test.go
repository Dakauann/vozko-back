package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
	"vozko/usecases/copilot/skills"
)

const (
	photoID = "0d6f3a2e-1b4c-4d8e-9f10-2a3b4c5d6e7f"
	clipID  = "1e7a4b3f-2c5d-4e9f-8a21-3b4c5d6e7f80"
	songID  = "2f8b5c4a-3d6e-4fa0-9b32-4c5d6e7f8091"
	pdfID   = "3a9c6d5b-4e7f-4ab1-8c43-5d6e7f8091a2"
)

type mediaShelf map[string]*media.Media

func (l mediaShelf) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	if m, ok := l[mediaID]; ok && workspaceID == "ws-1" {
		return m, nil
	}
	return nil, media.ErrMediaNotFound
}

func testLibrary() mediaShelf {
	return mediaShelf{
		photoID: {ID: photoID, URL: "https://cdn/photo.png", Type: media.MediaTypeProductImage},
		clipID:  {ID: clipID, URL: "https://cdn/clip.mp4", Type: media.MediaTypeProductVideo},
		songID:  {ID: songID, URL: "https://cdn/song.mp3", Type: media.MediaTypeAudio},
		pdfID:   {ID: pdfID, URL: "https://cdn/doc.pdf", Type: media.MediaTypeDocumentPdf},
	}
}

func newProjectTool() copilot.Tool {
	return NewNewStudioProjectTool(StudioDeps{Library: testLibrary()})
}

const createdProjectID = "6a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

func panelContext() copilot.Context {
	return tooltest.OwnerWithScreen(copilot.View{}, &tooltest.ProjectScreen{ProjectID: createdProjectID, Opened: true})
}

func orderOf(t *testing.T, res copilot.Result) *copilot.StudioProjectOrder {
	t.Helper()
	if res.Status != copilot.StatusOK || res.Card == nil || res.Card.Kind != copilot.ActionStudioProject || res.Card.StudioProject == nil {
		t.Fatalf("got %+v", res)
	}
	return res.Card.StudioProject
}

func TestAnEmptyImageProjectFollowsTheNewProjectDefaults(t *testing.T) {
	order := orderOf(t, newProjectTool().Execute(context.Background(), panelContext(), map[string]interface{}{"kind": "image"}))
	if order.Kind != "image" || len(order.Media) != 0 || len(order.Sizes) != 0 || order.Name != "" {
		t.Fatalf("an empty order lets the editor apply the same defaults as the new project dialog, got %+v", order)
	}
}

func TestAnEmptyVideoProjectTakesTheAspect(t *testing.T) {
	order := orderOf(t, newProjectTool().Execute(context.Background(), panelContext(), map[string]interface{}{"kind": "video", "sizes": []interface{}{"portrait"}, "name": "  Teaser   do  lançamento "}))
	if order.Aspect != "portrait" || order.Name != "Teaser do lançamento" || len(order.Sizes) != 0 {
		t.Fatalf("got %+v", order)
	}
}

func TestAnImageProjectFromMediaGetsOneArtboardPerPlacementSize(t *testing.T) {
	args := map[string]interface{}{"kind": "image", "media_ids": []interface{}{photoID}, "sizes": []interface{}{"feed", "vertical", "1080x1080", "1440X1800"}}
	order := orderOf(t, newProjectTool().Execute(context.Background(), panelContext(), args))
	want := []copilot.CanvasSize{{Width: 1440, Height: 1800}, {Width: 1440, Height: 2560}, {Width: 1080, Height: 1080}}
	if len(order.Sizes) != len(want) {
		t.Fatalf("repeated sizes collapse into one artboard, got %+v", order.Sizes)
	}
	for i := range want {
		if order.Sizes[i] != want[i] {
			t.Fatalf("sizes %+v", order.Sizes)
		}
	}
	if len(order.Media) != 1 || order.Media[0].URL != "https://cdn/photo.png" || order.Media[0].Type != "image" {
		t.Fatalf("media %+v", order.Media)
	}
}

func TestAVideoProjectFromMediaPlacesEveryKind(t *testing.T) {
	args := map[string]interface{}{"kind": "video", "media_ids": []interface{}{photoID, clipID, songID}, "duration_ms": 4000}
	order := orderOf(t, newProjectTool().Execute(context.Background(), panelContext(), args))
	types := []string{}
	for _, m := range order.Media {
		types = append(types, m.Type)
	}
	if strings.Join(types, ",") != "image,video,audio" || order.DurationMs != 4000 || order.Aspect != "" {
		t.Fatalf("got %+v", order)
	}
}

func TestTheNewProjectRefusesWhatTheEditorWouldRefuse(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"unknown kind":          {"kind": "audio"},
		"missing kind":          {},
		"tiny artboard":         {"kind": "image", "sizes": []interface{}{"50x1080"}},
		"huge artboard":         {"kind": "image", "sizes": []interface{}{"5000x1080"}},
		"unknown size":          {"kind": "image", "sizes": []interface{}{"banner"}},
		"video with pixels":     {"kind": "video", "sizes": []interface{}{"1080x1920"}},
		"two video aspects":     {"kind": "video", "sizes": []interface{}{"story", "square"}},
		"image with duration":   {"kind": "image", "duration_ms": 3000},
		"duration too long":     {"kind": "video", "duration_ms": 700000},
		"video in image":        {"kind": "image", "media_ids": []interface{}{clipID}},
		"document":              {"kind": "video", "media_ids": []interface{}{pdfID}},
		"invented media":        {"kind": "image", "media_ids": []interface{}{"foto.png"}},
		"media of another team": {"kind": "image", "media_ids": []interface{}{"4b0d7e6c-5f80-4bc2-9d54-6e7f8091a2b3"}},
		"name too long":         {"kind": "image", "name": strings.Repeat("a", 121)},
	}
	for name, args := range cases {
		screen := &tooltest.ProjectScreen{ProjectID: createdProjectID, Opened: true}
		res := newProjectTool().Execute(context.Background(), tooltest.OwnerWithScreen(copilot.View{}, screen), args)
		if res.Status != copilot.StatusError || res.Card != nil || res.Message == "" || res.Opening != nil {
			t.Errorf("%s: must be refused with a reason, got %+v", name, res)
		}
		if len(screen.Commands) != 0 {
			t.Errorf("%s: a refused order must never reach the app, got %+v", name, screen.Commands)
		}
	}
}

func TestTheAppCreatesTheProjectAndTheResultSaysTheEditorIsOpen(t *testing.T) {
	screen := &tooltest.ProjectScreen{ProjectID: createdProjectID, Opened: true}
	args := map[string]interface{}{"kind": "image", "media_ids": []interface{}{photoID}, "sizes": []interface{}{"feed"}, "name": "Carrossel Aurea - Capa"}
	res := newProjectTool().Execute(context.Background(), tooltest.OwnerWithScreen(copilot.View{}, screen), args)
	order := orderOf(t, res)
	if len(screen.Commands) != 1 || screen.Commands[0].Name != copilot.ScreenCreateProject {
		t.Fatalf("the app creates the project from one create command, got %+v", screen.Commands)
	}
	sent, ok := screen.Commands[0].Args.(copilot.StudioProjectOrder)
	if !ok || sent.Kind != "image" || sent.ProjectID != "" || len(sent.Media) != 1 || sent.Name != "Carrossel Aurea - Capa" {
		t.Fatalf("the app receives the checked order, got %+v", screen.Commands[0].Args)
	}
	if order.ProjectID != createdProjectID {
		t.Fatalf("the card points at the created project, got %q", order.ProjectID)
	}
	want := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: createdProjectID, ProjectKind: copilot.StudioImage}
	if res.Opening == nil || !res.Opening.Opened || res.Opening.View != want {
		t.Fatalf("the result hands the open editor to the turn, got %+v", res.Opening)
	}
	data := tooltest.DataOf(res)
	if data["state"] != "opened" || data["project_id"] != createdProjectID || data["kind"] != "image" {
		t.Fatalf("the model reads the real state, got %+v", data)
	}
	if next, _ := data["next"].(string); !strings.Contains(next, "studio_read") || strings.Contains(next, "quando o usuário tocar") {
		t.Fatalf("an open editor means working now, never asking to open it, got %q", next)
	}
}

func TestWithAutomaticScreensOffTheResultSaysTheProjectWaitsToBeOpened(t *testing.T) {
	screen := &tooltest.ProjectScreen{ProjectID: createdProjectID}
	res := newProjectTool().Execute(context.Background(), tooltest.OwnerWithScreen(copilot.View{}, screen), map[string]interface{}{"kind": "video"})
	order := orderOf(t, res)
	if order.ProjectID != createdProjectID || res.Opening == nil || res.Opening.Opened || res.Opening.Awaiting() == nil {
		t.Fatalf("a created but closed project is awaited, got %+v %+v", order, res.Opening)
	}
	data := tooltest.DataOf(res)
	next, _ := data["next"].(string)
	if data["state"] != "waiting" || data["project_id"] != createdProjectID || data["kind"] != "video" {
		t.Fatalf("the model reads the real state, got %+v", data)
	}
	if !strings.Contains(next, "Abrir no Estúdio") || !strings.Contains(next, "o app avisa") || !strings.Contains(next, "nunca peça") {
		t.Fatalf("a waiting project is opened by the person and announced by the app, got %q", next)
	}
}

func TestTheNewProjectFailsClosedWhenTheAppCannotCreateIt(t *testing.T) {
	declined := copilot.ScreenReply{Error: &copilot.ScreenError{Code: copilot.ScreenNoEditor, Message: "sem conversa nesta aba"}}
	refused := copilot.ScreenReply{Error: &copilot.ScreenError{Code: "create_failed", Message: "imagem ilegível"}}
	garbled := copilot.ScreenReply{OK: true, Data: []byte(`{"projectId":"p1","kind":"image","opened":true}`)}
	cases := map[string]copilot.Context{
		"no app attached":  tooltest.OwnerOn(copilot.View{}),
		"silent app":       tooltest.OwnerWithScreen(copilot.View{}, &tooltest.ProjectScreen{Err: copilot.ErrScreenUnavailable}),
		"only other tabs":  tooltest.OwnerWithScreen(copilot.View{}, &tooltest.ProjectScreen{Reply: &declined}),
		"app refused":      tooltest.OwnerWithScreen(copilot.View{}, &tooltest.ProjectScreen{Reply: &refused}),
		"unusable project": tooltest.OwnerWithScreen(copilot.View{}, &tooltest.ProjectScreen{Reply: &garbled}),
	}
	for name, cc := range cases {
		res := newProjectTool().Execute(context.Background(), cc, map[string]interface{}{"kind": "image"})
		if res.Status != copilot.StatusError || res.Card != nil || res.Opening != nil || res.Message == "" {
			t.Errorf("%s: nothing is reported as created, got %+v", name, res)
		}
	}
	res := newProjectTool().Execute(context.Background(), cases["app refused"], map[string]interface{}{"kind": "image"})
	if !strings.Contains(res.Message, "imagem ilegível") {
		t.Fatalf("the app's reason reaches the model, got %q", res.Message)
	}
}

func TestTheNewProjectToolOpensTheEditorFromOutsideTheStudio(t *testing.T) {
	tool := newProjectTool()
	if tool.Meta().Mutating || tool.Meta().Resource != workspace.ResourceMedia || tool.Meta().Action != workspace.ActionCreate {
		t.Fatalf("meta %+v", tool.Meta())
	}
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioImage}
	if studio.Offers(tool) || !(copilot.View{}).Offers(tool) || !(copilot.View{Surface: copilot.SurfaceAttendance}).Offers(tool) {
		t.Fatal("the tool brings people into the studio, so it lives outside it")
	}
	res := tool.Execute(context.Background(), panelContext(), map[string]interface{}{"kind": "image"})
	if !strings.Contains(tooltest.DataOf(res)["next"].(string), "studio_") {
		t.Fatalf("the reply must say the studio tools arrive once the project is open, got %+v", res.Data)
	}
}

func TestTheSkillsTeachHerToReadTheCreatedProjectNeverToAskForIt(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"design-de-imagem", "criativo-com-a-marca"} {
		skill, ok := library.Find(name)
		if !ok {
			t.Fatalf("%s is missing", name)
		}
		for _, want := range []string{"state opened", "state waiting", "Abrir no Estúdio", "o app avisa", "Nunca peça"} {
			if !strings.Contains(skill.Body, want) {
				t.Errorf("%s must teach %q about a created project", name, want)
			}
		}
		if strings.Contains(skill.Body, "me avise") || strings.Contains(skill.Body, "próxima mensagem") {
			t.Errorf("%s still makes her wait for the person to report the editor", name)
		}
	}
}
