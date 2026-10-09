package copilottools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
)

const threadCharge = "aichat:th-1"

func charged(cc copilot.Context) copilot.Context {
	cc.ChargeReference = threadCharge
	return cc
}

func TestAGenerationEloStartsIsChargedToItsThread(t *testing.T) {
	images := &stubImages{final: &mediagen.Job{ID: "job-1", Kind: mediagen.KindImage, Status: mediagen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg"}}
	if res := NewGenerateImageTool(images).Execute(context.Background(), charged(imageSession), imageArgs()); res.Status != copilot.StatusOK {
		t.Fatalf("result %+v", res)
	}
	if images.requested.BillingReference != threadCharge {
		t.Fatalf("generate_image must charge the thread, got %q", images.requested.BillingReference)
	}
}

func TestAStudioJobEloStartsIsChargedToItsThread(t *testing.T) {
	media := &stubImages{}
	tool := NewStudioStartJobTool(studioDeps(studio.KindVideo, media, &fakeJobs{}))
	res := tool.Execute(context.Background(), charged(studioSession(copilot.StudioVideo, startJobEditor())), map[string]interface{}{"kind": "captions", "clip_id": "c-1"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result %+v", res)
	}
	if media.requested.BillingReference != threadCharge {
		t.Fatalf("studio_start_job must charge the thread, got %q", media.requested.BillingReference)
	}
}

func TestAStudioGenerationEloStartsIsChargedToItsThread(t *testing.T) {
	media := &stubImages{}
	tool := NewStudioGenerateMusicTool(studioDeps(studio.KindVideo, media, &fakeJobs{}))
	args := map[string]interface{}{"prompt": "violão leve para café", "music_model": "google/lyria-3-clip-preview"}
	res := tool.Execute(context.Background(), charged(studioSession(copilot.StudioVideo, &fakeEditor{})), args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("result %+v", res)
	}
	if media.requested.BillingReference != threadCharge {
		t.Fatalf("studio_generate_music must charge the thread, got %q", media.requested.BillingReference)
	}
}
