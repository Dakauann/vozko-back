package mediatools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func TestMusicIsChosenOnTheCardWithTheRecommendedModelMarked(t *testing.T) {
	choices, err := NewGenerateMusicTool(&tooltest.StubImages{}).(copilot.ChoiceAsker).Choices(context.Background(), tooltest.ImageSession, nil)
	if err != nil || len(choices) != 1 || choices[0].Kind != copilot.ChoiceMusicModel || choices[0].Default != "google/lyria-3-pro-preview" {
		t.Fatalf("choices %+v err %v", choices, err)
	}
	failing := &tooltest.StubImages{CatalogErr: errors.New("openrouter down")}
	if _, err := NewGenerateVoiceoverTool(failing).(copilot.ChoiceAsker).Choices(context.Background(), tooltest.ImageSession, nil); err == nil {
		t.Fatal("offered a card without a model list")
	}
}

func TestAVoiceOverRequestsTheExactScriptWithTheChosenModel(t *testing.T) {
	media := &tooltest.StubImages{Final: &mediagen.Job{ID: "job-1", Kind: mediagen.KindVoice, Status: mediagen.StatusDone, MediaID: "m-9", MediaURL: "https://cdn/v.m4a"}}
	args := map[string]interface{}{"script": "Conheça a Vozko.", "voice": "coral", "voice_model": "openai/gpt-audio-mini"}
	tool := NewGenerateVoiceoverTool(media)
	if err := tool.(copilot.Validator).Validate(context.Background(), tooltest.ImageSession, args); err != nil {
		t.Fatal(err)
	}
	res := tool.Execute(context.Background(), tooltest.ImageSession, args)
	req := media.Requested
	if req.Kind != mediagen.KindVoice || req.Prompt != "Conheça a Vozko." || req.Voice != "coral" || req.Model != "openai/gpt-audio-mini" {
		t.Fatalf("requested %+v", req)
	}
	if res.Status != copilot.StatusOK || res.Media == nil || res.Media.Kind != copilot.MediaAudio || res.Media.MediaID != "m-9" {
		t.Fatalf("result %+v", res)
	}
}

func TestAnAudioWithoutAChosenModelIsNotQueued(t *testing.T) {
	media := &tooltest.StubImages{}
	res := NewGenerateMusicTool(media).Execute(context.Background(), tooltest.ImageSession, map[string]interface{}{"prompt": "samba"})
	if res.Status != copilot.StatusError || media.RequestedBy != "" {
		t.Fatalf("result %+v", res)
	}
}

func TestAResultStillSettlingIsNotHandedOut(t *testing.T) {
	media := &tooltest.StubImages{Final: &mediagen.Job{ID: "job-1", Kind: mediagen.KindMusic, Status: mediagen.StatusSettling, MediaID: "m-1"}, WaitErr: context.DeadlineExceeded}
	res := NewGenerateMusicTool(media).Execute(context.Background(), tooltest.ImageSession, map[string]interface{}{"prompt": "samba", "music_model": "google/lyria-3-clip-preview"})
	if res.Status != copilot.StatusError || res.Media != nil {
		t.Fatalf("result %+v", res)
	}
}

const (
	sceneOne = "1b2c3d4e-0000-4000-8000-000000000001"
	sceneTwo = "1b2c3d4e-0000-4000-8000-000000000002"
	songID   = "1b2c3d4e-0000-4000-8000-000000000003"
)

func renderArgs() map[string]interface{} {
	return map[string]interface{}{
		"aspect":         "story",
		"scenes":         []interface{}{map[string]interface{}{"media_id": sceneOne, "seconds": 4.0}, map[string]interface{}{"media_id": sceneTwo, "seconds": 6.0}},
		"music_media_id": songID,
	}
}

func TestRenderVideoPlansTheScenesAndSoundWithoutAModel(t *testing.T) {
	media := &tooltest.StubImages{Library: map[string]string{sceneOne: "https://cdn/1.png", sceneTwo: "https://cdn/2.png", songID: "https://cdn/s.m4a"},
		Final: &mediagen.Job{ID: "job-1", Kind: mediagen.KindVideo, Status: mediagen.StatusDone, MediaID: "v-1", MediaURL: "https://cdn/v.mp4"}}
	tool := NewRenderVideoTool(media)
	if err := tool.(copilot.Validator).Validate(context.Background(), tooltest.ImageSession, renderArgs()); err != nil {
		t.Fatal(err)
	}
	preview := tool.(copilot.Previewer).Preview(context.Background(), tooltest.ImageSession, renderArgs())
	plan, _ := preview.Data.(VideoPlanPreview)
	if preview.Kind != PreviewVideoPlan || len(plan.Scenes) != 2 || plan.Scenes[1].Seconds != 6 || plan.Music == nil || plan.Music.URL != "https://cdn/s.m4a" {
		t.Fatalf("preview %+v", preview)
	}
	res := tool.Execute(context.Background(), tooltest.ImageSession, renderArgs())
	if res.Status != copilot.StatusOK || res.Media.Kind != copilot.MediaVideo || media.Requested.Model != "" || media.Requested.Video.DurationMS != 10_000 {
		t.Fatalf("result %+v requested %+v", res, media.Requested)
	}
}

func TestRenderVideoRefusesMediaThatIsNotInTheLibrary(t *testing.T) {
	media := &tooltest.StubImages{Library: map[string]string{sceneOne: "https://cdn/1.png"}}
	if err := NewRenderVideoTool(media).(copilot.Validator).Validate(context.Background(), tooltest.ImageSession, renderArgs()); !errors.Is(err, copilottools.ErrInvalidArgs) {
		t.Fatalf("got %v", err)
	}
}

func TestTheCardsNameTheScriptAndTheSoundWithoutRawIDs(t *testing.T) {
	voice := tooltest.FieldMap(NewGenerateVoiceoverTool(&tooltest.StubImages{}).(copilot.Describer).Describe(context.Background(), tooltest.ImageSession, map[string]interface{}{"script": "Olá", "voice": "coral"}))
	if voice["script"] != "Olá" || voice["voice"] != "coral" {
		t.Fatalf("voice card %+v", voice)
	}
	video := tooltest.FieldMap(NewRenderVideoTool(&tooltest.StubImages{}).(copilot.Describer).Describe(context.Background(), tooltest.ImageSession, renderArgs()))
	if video["sound"] != "música" {
		t.Fatalf("video card %+v", video)
	}
}

func TestEveryFailureCodeHasItsOwnMessage(t *testing.T) {
	for _, code := range []mediagen.FailureCode{mediagen.FailureGeneration, mediagen.FailureStorage, mediagen.FailureTimedOut, mediagen.FailureEnqueue, mediagen.FailureInsufficientFunds, mediagen.FailureReferenceUnavailable, mediagen.FailureCostUnreported, mediagen.FailureProviderBusy} {
		if MediaFailureMessage(code) == MediaFailedUnknown {
			t.Errorf("%s falls back to the unknown failure message", code)
		}
	}
}
