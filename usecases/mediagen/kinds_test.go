package mediagen_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/media"
	"vozko/domain/mediagen"
)

func musicRequest() mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindMusic, WorkspaceID: "ws-1", Model: musicModel, Prompt: "samba leve para cafeteria"}
}

func videoRequest() mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: "ws-1", Aspect: mediagen.AspectStory, Video: mediagen.SlideshowTimeline(
		[]mediagen.Scene{{MediaID: "ref-1", Seconds: 4}, {MediaID: "clip", Seconds: 6}}, "song", "",
	)}
}

func run(t *testing.T, f *fixture, req mediagen.Request) mediagen.Job {
	t.Helper()
	job, err := f.svc.Request(context.Background(), req, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	return f.jobs.job(job.ID)
}

func TestMusicIsBilledWithItsModelAndStoredAsAudio(t *testing.T) {
	f := newFixture(t)
	f.audio.cost = 40_000
	job := run(t, f, musicRequest())
	if job.Status != mediagen.StatusDone || f.audio.calls != 1 || f.gen.calls != 0 {
		t.Fatalf("job %+v", job)
	}
	if len(f.bill.events) != 1 || f.bill.events[0].model != musicModel || f.bill.events[0].cost != 40_000 {
		t.Fatalf("billing %+v", f.bill.events)
	}
	if f.up.kinds[0] != media.MediaTypeAudio || !strings.HasPrefix(f.up.names[0], "audio/ws-1/") || !strings.HasSuffix(f.up.names[0], ".m4a") {
		t.Fatalf("stored %v %v", f.up.names, f.up.kinds)
	}
}

func TestAMusicModelOutsideTheAudioCatalogIsRefused(t *testing.T) {
	f := newFixture(t)
	req := musicRequest()
	req.Model = imageModel
	if _, err := f.svc.Request(context.Background(), req, "u-1"); modelCode(t, err) != mediagen.CodeUnknown {
		t.Fatalf("got %v", err)
	}
}

func TestAVideoRendersOnItsOwnTopicWithoutBillingAI(t *testing.T) {
	f := newFixture(t)
	job := run(t, f, videoRequest())
	if job.Status != mediagen.StatusDone || f.video.calls != 1 || len(f.bill.events) != 0 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
	if f.queue.topics[0] != mediagen.RenderTopic {
		t.Fatalf("topics %v", f.queue.topics)
	}
	got := f.video.references
	if len(got) != 3 || got[0].Type != media.MediaTypeProductImage || got[1].Type != media.MediaTypeProductVideo || got[2].MediaID != "song" {
		t.Fatalf("sources %+v", got)
	}
	if f.up.kinds[0] != media.MediaTypeProductVideo || !strings.HasSuffix(f.up.names[0], ".mp4") {
		t.Fatalf("stored %v %v", f.up.names, f.up.kinds)
	}
}

func TestAVideoOnlyReadsMediaOfTheRightTypeFromThisWorkspace(t *testing.T) {
	cases := map[string]struct {
		change func(*mediagen.Request)
		field  string
		code   string
	}{
		"image as music":      {func(r *mediagen.Request) { r.Video.Audio[0].Clips[0].MediaID = "ref-2" }, mediagen.FieldTimeline, mediagen.CodeWrongType},
		"song as a scene":     {func(r *mediagen.Request) { r.Video.Visual[0].Clips[0].MediaID = "song" }, mediagen.FieldTimeline, mediagen.CodeWrongType},
		"another workspace's": {func(r *mediagen.Request) { r.Video.Visual[0].Clips[0].MediaID = "other" }, mediagen.FieldTimeline, mediagen.CodeNotFound},
		"missing sound":       {func(r *mediagen.Request) { r.Video.Audio[0].Clips[0].MediaID = "nope" }, mediagen.FieldTimeline, mediagen.CodeNotFound},
	}
	for name, c := range cases {
		f := newFixture(t)
		req := videoRequest()
		c.change(&req)
		_, err := f.svc.Request(context.Background(), req, "u-1")
		var invalid *mediagen.ValidationError
		if !errors.As(err, &invalid) || invalid.Codes()[c.field] != c.code {
			t.Errorf("%s: got %v", name, err)
		}
		if len(f.jobs.jobs) != 0 || len(f.queue.ids) != 0 {
			t.Errorf("%s: a job was queued", name)
		}
	}
}

func TestARenderHasNoModelList(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Models(context.Background(), mediagen.KindVideo); !errors.Is(err, mediagen.ErrNoModels) {
		t.Fatalf("got %v", err)
	}
	models, err := f.svc.Models(context.Background(), mediagen.KindMusic)
	if err != nil || len(models) != 1 || models[0].ID != musicModel {
		t.Fatalf("music models %+v err %v", models, err)
	}
}

func cutoutRequest(source string) mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindCutout, WorkspaceID: "ws-1", SourceMediaID: source}
}

func TestABackgroundRemovalIsChargedThroughTheProcessingPriceAndStoredAsPNG(t *testing.T) {
	f := newFixture(t)
	job := run(t, f, cutoutRequest("ref-1"))
	if job.Status != mediagen.StatusDone || len(f.charges.charged) != 1 || len(f.bill.events) != 0 {
		t.Fatalf("job %+v charged %v billed %v", job, f.charges.charged, f.bill.events)
	}
	if !strings.HasSuffix(f.up.names[0], ".png") || f.queue.topics[0] != mediagen.RenderTopic {
		t.Fatalf("stored %v topics %v", f.up.names, f.queue.topics)
	}
}

func TestAProcessingJobThatCannotBeChargedDeliversNothing(t *testing.T) {
	f := newFixture(t)
	f.charges.err = errors.New("no balance")
	job := run(t, f, cutoutRequest("ref-1"))
	if job.Status != mediagen.StatusFailed || len(f.up.names) != 0 {
		t.Fatalf("job %+v stored %v", job, f.up.names)
	}
}

func TestAWorkspaceRunsAtMostTwoProcessingJobsAtOnce(t *testing.T) {
	f := newFixture(t)
	for _, source := range []string{"ref-1", "ref-2"} {
		if _, err := f.svc.Request(context.Background(), cutoutRequest(source), "u-1"); err != nil {
			t.Fatal(err)
		}
	}
	again, err := f.svc.Request(context.Background(), cutoutRequest("ref-1"), "u-1")
	if err != nil || len(f.jobs.jobs) != 2 || again == nil {
		t.Fatalf("a repeated click returns the running job: %v", err)
	}
	if _, err := f.svc.Request(context.Background(), mediagen.Request{Kind: mediagen.KindDenoise, WorkspaceID: "ws-1", SourceMediaID: "song"}, "u-1"); !errors.Is(err, mediagen.ErrTooManyActive) {
		t.Fatalf("a third job got %v", err)
	}
	if _, err := f.svc.Request(context.Background(), musicRequest(), "u-1"); err != nil {
		t.Fatalf("AI generations are not capped by processing: %v", err)
	}
}

func TestTheServiceTellsWhetherAProcessingKindIsPriced(t *testing.T) {
	charges := &fakeCharges{priced: map[mediagen.Kind]bool{mediagen.KindCaptions: true}}
	svc := &Service{d: Deps{Charges: charges}}
	if !svc.ProcessingPriced(mediagen.KindCaptions) || svc.ProcessingPriced(mediagen.KindCutout) {
		t.Fatal("the answer must come from the charges port")
	}
	if !svc.ProcessingPriced(mediagen.KindMusic) {
		t.Fatal("a kind that uses a model is always paid")
	}
}

func TestAWorkspaceRunsAtMostThreeAIGenerationsAtOnce(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < mediagen.MaxActiveGenerations; i++ {
		req := musicRequest()
		req.Prompt += strings.Repeat(" mais", i+1)
		if _, err := f.svc.Request(context.Background(), req, "u-1"); err != nil {
			t.Fatalf("generation %d: %v", i+1, err)
		}
	}
	extra := musicRequest()
	extra.Prompt += " outra"
	if _, err := f.svc.Request(context.Background(), extra, "u-1"); !errors.Is(err, mediagen.ErrTooManyActive) {
		t.Fatalf("a generation past the ceiling got %v", err)
	}
	if _, err := f.svc.Request(context.Background(), cutoutRequest("ref-1"), "u-1"); err != nil {
		t.Fatalf("processing has its own ceiling: %v", err)
	}
}
