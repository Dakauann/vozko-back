package advertising

import (
	"slices"
	"testing"
)

func runAll(t *testing.T, j *PublishJob) []string {
	t.Helper()
	var keys []string
	for {
		step, ok := j.NextStep()
		if !ok {
			return keys
		}
		keys = append(keys, step.Key())
		j.Begin(step)
		if err := j.Complete(step, "id-"+step.Key()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStepsRunInMetaDependencyOrder(t *testing.T) {
	d := validDraft()
	d.Ads = append(d.Ads, AdItem{Name: "Vídeo", Creative: CreativeDraft{Format: FormatVideo, PrimaryText: "x", Media: MediaRef{Kind: MediaVideo, MediaID: "v1"}}})
	j := &PublishJob{Draft: d}
	got := runAll(t, j)
	want := []string{"media:media-1", "media:v1", "video_ready:v1", "campaign", "adset", "creative:0", "ad:0", "creative:1", "ad:1", "activate"}
	if !slices.Equal(got, want) {
		t.Fatalf("steps %v", got)
	}
}

func TestSharedMediaIsUploadedOnce(t *testing.T) {
	d := validDraft()
	d.Ads = append(d.Ads, AdItem{Name: "B", Creative: imageCreative()})
	steps := (&PublishJob{Draft: d}).Steps()
	if steps[0].Key() != "media:media-1" || steps[1].Kind != StepCampaign {
		t.Fatalf("steps %v", steps)
	}
}

func TestAddingToAnExistingAdSetCreatesOnlyAdsAndActivatesOnlyThem(t *testing.T) {
	d := validDraft()
	d.Campaign.ExistingID, d.AdSet.ExistingID = "c-9", "s-9"
	j := &PublishJob{Draft: d}
	got := runAll(t, j)
	if slices.Contains(got, "campaign") || slices.Contains(got, "adset") {
		t.Fatalf("parents recreated: %v", got)
	}
	if j.AdSetID() != "s-9" || !slices.Equal(j.ToActivate(), []string{"id-ad:0"}) {
		t.Fatalf("adset %s activate %v", j.AdSetID(), j.ToActivate())
	}
}

func TestKeepPausedSkipsActivation(t *testing.T) {
	d := validDraft()
	d.KeepPaused = true
	if slices.Contains(runAll(t, &PublishJob{Draft: d}), "activate") {
		t.Fatal("paused draft activated")
	}
}

func TestResumeSkipsStepsMetaAlreadyConfirmed(t *testing.T) {
	j := &PublishJob{Draft: validDraft(), Progress: Progress{Media: map[string]string{"media-1": "h"}, CampaignID: "c"}}
	if step, _ := j.NextStep(); step.Kind != StepAdSet {
		t.Fatalf("next %s", step.Key())
	}
}

func TestCrashDuringACreateIsAmbiguousButDuringUploadOrActivateIsNot(t *testing.T) {
	for _, s := range []Step{{Kind: StepCampaign}, {Kind: StepAdSet}, {Kind: StepCreative}, {Kind: StepAd}} {
		j := &PublishJob{Draft: validDraft()}
		j.Begin(s)
		if !j.Interrupted() {
			t.Fatalf("%s crash would be retried and could duplicate the object", s.Key())
		}
	}
	for _, s := range []Step{{Kind: StepMedia, Media: MediaRef{Kind: MediaImage, MediaID: "media-1"}}, {Kind: StepActivate}} {
		j := &PublishJob{Draft: validDraft()}
		j.Begin(s)
		if j.Interrupted() {
			t.Fatalf("%s is safe to retry", s.Key())
		}
	}
	unknown := &PublishJob{Draft: validDraft(), Progress: Progress{InFlight: "mystery"}}
	if !unknown.Interrupted() {
		t.Fatal("unknown in-flight step treated as safe")
	}
}

func TestCompleteRefusesAMissingIdOrTheWrongStep(t *testing.T) {
	j := &PublishJob{Draft: validDraft()}
	j.Begin(Step{Kind: StepCampaign})
	if err := j.Complete(Step{Kind: StepCampaign}, ""); err == nil {
		t.Fatal("empty campaign id accepted")
	}
	if err := j.Complete(Step{Kind: StepAd}, "x"); err == nil {
		t.Fatal("wrong step accepted")
	}
}

func TestUploadedMediaSplitsImagesFromVideos(t *testing.T) {
	d := validDraft()
	d.Ads[0].Creative = CreativeDraft{Format: FormatCarousel, PrimaryText: "x", Cards: []CarouselCard{
		{Media: MediaRef{Kind: MediaImage, MediaID: "i1"}}, {Media: MediaRef{Kind: MediaVideo, MediaID: "v1"}},
	}}
	j := &PublishJob{Draft: d, Progress: Progress{Media: map[string]string{"i1": "hash", "v1": "vid"}}}
	m := j.UploadedMedia()
	if m.ImageHash("i1") != "hash" || m.VideoID("v1") != "vid" || m.ImageHash("v1") != "" {
		t.Fatalf("media %+v", m)
	}
}

func TestRefundOnlyForDefiniteFailuresBeforeTheAdWentLive(t *testing.T) {
	j := &PublishJob{Fee: FeeCharged}
	j.Fail("meta_rejected", "x")
	if !j.RefundDue() {
		t.Fatal("definite failure not refunded")
	}
	review := &PublishJob{Fee: FeeCharged}
	review.Review("ambiguous", "x")
	live := &PublishJob{Fee: FeeCharged, Progress: Progress{Activated: true}}
	live.Fail("x", "y")
	refunded := &PublishJob{Fee: FeeRefunded}
	refunded.Fail("x", "y")
	if review.RefundDue() || live.RefundDue() || refunded.RefundDue() {
		t.Fatal("refund paid where it should not be")
	}
}

func TestCleanupDeletesChildrenBeforeParentsAndNeverExistingParents(t *testing.T) {
	j := &PublishJob{Draft: validDraft(), Progress: Progress{CampaignID: "c", AdSetID: "s", Ads: map[int]string{0: "a"}}}
	if got := j.CreatedObjects(); !slices.Equal(got, []string{"a", "s", "c"}) {
		t.Fatalf("cleanup order %v", got)
	}
	existing := validDraft()
	existing.Campaign.ExistingID, existing.AdSet.ExistingID = "c", "s"
	j = &PublishJob{Draft: existing, Progress: Progress{Ads: map[int]string{0: "a"}}}
	if got := j.CreatedObjects(); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("cleanup touched parents %v", got)
	}
}
