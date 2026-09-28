package facebook

import (
	"context"
	"errors"
	"testing"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

func TestAVideoWaitsForProcessingAndCompletesFromTheWebhook(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishVideo, VideoTitle: "Lançamento", Message: "Veja", Media: []fbdomain.MediaRef{mp4Ref("https://r2/v.mp4")}})

	waiting, err := f.run(t, job.ID)
	if err != nil || waiting.Status != fbdomain.JobProcessing || waiting.FBObjectID != "V1" || waiting.NextCheckAt == nil {
		t.Fatalf("job %+v %v", waiting, err)
	}
	if in := f.service.videos[0]; in.FileURL != "https://r2/v.mp4" || in.Title != "Lançamento" || in.Description != "Veja" {
		t.Fatalf("video input %+v", in)
	}

	f.service.statuses["V1"] = &fbdomain.VideoStatus{State: "ready", PostID: "fb-page-1_V1"}
	if err := f.worker.ResolveVideo(context.Background(), "V1", "ready"); err != nil {
		t.Fatal(err)
	}
	done, _ := f.jobs.FindByID(context.Background(), job.ID)
	if done.Status != fbdomain.JobPublished || done.FBPostID != "fb-page-1_V1" {
		t.Fatalf("job %+v", done)
	}
	if post := f.posts.byFBID["fb-page-1_V1"]; post == nil || post.Kind != fbdomain.PostVideo || !post.CreatedByApp {
		t.Fatalf("mirror %+v", post)
	}
}

func TestAFailedVideoFailsItsJob(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishVideo, Media: []fbdomain.MediaRef{mp4Ref("https://r2/v.mp4")}})
	_, _ = f.run(t, job.ID)

	if err := f.worker.ResolveVideo(context.Background(), "V1", "error"); err != nil {
		t.Fatal(err)
	}
	if done, _ := f.jobs.FindByID(context.Background(), job.ID); done.Status != fbdomain.JobFailed {
		t.Fatalf("job %+v", done)
	}
}

func TestAWebhookForAnUnknownVideoIsIgnored(t *testing.T) {
	f := newPostFixture()
	if err := f.worker.ResolveVideo(context.Background(), "nobody", "ready"); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestAReelStartsTransfersFinishesAndResumes(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishReel, Message: "novidade", Media: []fbdomain.MediaRef{mp4Ref("https://r2/r.mp4")}})
	f.service.transferErrs = []error{&meta.Error{Code: meta.CodePageRateLimit}}

	first, err := f.run(t, job.ID)
	if err == nil || first.Status != fbdomain.JobQueued || first.Progress.VideoID != "R1" {
		t.Fatalf("after throttled transfer: %+v %v", first, err)
	}

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobProcessing || done.FBPostID != "fb-page-1_R1" {
		t.Fatalf("job %+v %v", done, err)
	}
	if len(f.service.sessions) != 1 || f.service.sessions[0] != fbdomain.VideoForReel {
		t.Fatalf("a resumed reel opened another upload session: %v", f.service.sessions)
	}
	if len(f.service.transfers) != 2 || len(f.service.finishes) != 1 || f.service.finishes[0] != "reel:R1:novidade" {
		t.Fatalf("transfers %v finishes %v", f.service.transfers, f.service.finishes)
	}
}

func TestReelDailyCapIsRefusedUpFront(t *testing.T) {
	f := newPostFixture()
	f.jobs.reelCount = fbdomain.MaxReelsPerDay
	_, err := f.uc.Create(context.Background(), "ws-1", "page-1", "u", fbdomain.PublishRequest{Kind: fbdomain.PublishReel, Media: []fbdomain.MediaRef{mp4Ref("https://r2/r.mp4")}})
	if !errors.Is(err, fbdomain.ErrReelLimit) {
		t.Fatalf("got %v", err)
	}
}

func TestMetasReelCapFailsTheJob(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishReel, Media: []fbdomain.MediaRef{mp4Ref("https://r2/r.mp4")}})
	f.service.finishErr = &meta.Error{Code: meta.CodeMessagingRate, Message: "reel limit"}

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobFailed || done.ErrorCode != fbdomain.CodeReelCap || done.ErrorMessage != fbdomain.ErrReelLimit.Error() {
		t.Fatalf("job %+v %v", done, err)
	}
}

func TestAPhotoStoryPublishesThroughAnUnpublishedPhoto(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishStory, Media: []fbdomain.MediaRef{photoRef("https://r2/s.jpg")}})

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobPublished || done.FBPostID != "fb-page-1_story" {
		t.Fatalf("job %+v %v", done, err)
	}
	if f.service.photos[0].Published || f.service.storyPhotos[0] != "photo1" {
		t.Fatalf("photos %+v stories %v", f.service.photos, f.service.storyPhotos)
	}
}

func TestAVideoStoryUsesTheStoryUploadSession(t *testing.T) {
	f := newPostFixture()
	job := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishStory, Media: []fbdomain.MediaRef{mp4Ref("https://r2/s.mp4")}})

	done, err := f.run(t, job.ID)
	if err != nil || done.Status != fbdomain.JobProcessing || f.service.sessions[0] != fbdomain.VideoForStory || f.service.finishes[0] != "story:R1" {
		t.Fatalf("job %+v %v sessions %v finishes %v", done, err, f.service.sessions, f.service.finishes)
	}
}

func TestTheReaperPollsDueVideosAndGivesUpAfterTwoHours(t *testing.T) {
	f := newPostFixture()
	ready := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishVideo, Media: []fbdomain.MediaRef{mp4Ref("https://r2/a.mp4")}})
	stuck := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishVideo, Media: []fbdomain.MediaRef{mp4Ref("https://r2/b.mp4")}})
	pending := f.create(t, fbdomain.PublishRequest{Kind: fbdomain.PublishVideo, Media: []fbdomain.MediaRef{mp4Ref("https://r2/c.mp4")}})
	for _, id := range []string{ready.ID, stuck.ID, pending.ID} {
		_, _ = f.run(t, id)
	}
	past := time.Now().UTC().Add(-time.Minute)
	for _, j := range f.jobs.byID {
		j.NextCheckAt = &past
	}
	f.jobs.byID[stuck.ID].CreatedAt = time.Now().UTC().Add(-3 * time.Hour)
	f.jobs.byID[pending.ID].CreatedAt = time.Now().UTC()
	f.service.statuses["V1"] = &fbdomain.VideoStatus{State: "ready", PostID: "fb-page-1_V1"}

	if err := f.worker.Reap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if j := f.jobs.byID[ready.ID]; j.Status != fbdomain.JobPublished {
		t.Errorf("ready video %+v", j)
	}
	if j := f.jobs.byID[stuck.ID]; j.Status != fbdomain.JobFailed {
		t.Errorf("stuck video %+v", j)
	}
	if j := f.jobs.byID[pending.ID]; j.Status != fbdomain.JobProcessing || j.NextCheckAt == nil || !j.NextCheckAt.After(time.Now().UTC()) {
		t.Errorf("pending video %+v", j)
	}
}
