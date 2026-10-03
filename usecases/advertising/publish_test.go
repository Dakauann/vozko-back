package advertising

import (
	"context"
	"errors"
	"slices"
	"testing"

	ads "vozko/domain/advertising"
)

func publish(t *testing.T, w *world, draft ads.AdDraft) (*ads.PublishJob, error) {
	t.Helper()
	return w.publisher().Publish(context.Background(), PublishInput{WorkspaceID: "ws-1", UserID: "u-1", Actor: ads.ActorPerson, Draft: draft})
}

func metaWrites(calls []string) []string {
	var out []string
	for _, c := range calls {
		switch c {
		case "list_pages", "list_campaign", "list_adset", "list_ad", "minimum_budgets":
			continue
		}
		out = append(out, c)
	}
	return out
}

func TestPublishChargesThenCreatesEverythingPausedThenActivates(t *testing.T) {
	w := newWorld()
	job, err := publish(t, w, publishableDraft())
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != ads.JobPublished || job.Fee != ads.FeeCharged || !slices.Equal(w.fees.quantities, []int{1}) {
		t.Fatalf("job %+v fees %+v", job, w.fees)
	}
	want := []string{"image", "campaign", "adset", "creative", "ad", "status:c-1", "status:s-1", "status:a-1"}
	if got := metaWrites(w.gateway.calls); !slices.Equal(got, want) {
		t.Fatalf("calls %v", got)
	}
	if w.gateway.campaigns[0].Status != ads.StatusPaused || w.gateway.adSets[0].PromotedObject.WhatsAppPhoneNumber != "5511988887777" {
		t.Fatalf("specs %+v %+v", w.gateway.campaigns[0], w.gateway.adSets[0])
	}
}

func TestEachAdInADraftIsChargedAndCreated(t *testing.T) {
	w := newWorld()
	d := publishableDraft()
	d.Ads = append(d.Ads, ads.AdItem{Name: "Vídeo", Creative: ads.CreativeDraft{Format: ads.FormatVideo, PrimaryText: "x", Media: ads.MediaRef{Kind: ads.MediaVideo, MediaID: "v1"}}})
	w.gateway.videoStates = []ads.VideoState{ads.VideoProcessing, ads.VideoReady}
	job, err := publish(t, w, d)
	if err != nil || job.Status != ads.JobPublished {
		t.Fatalf("job %+v err %v", job, err)
	}
	if !slices.Equal(w.fees.quantities, []int{2}) || len(w.gateway.creatives) != 2 || w.gateway.creatives[1].Media.VideoID("v1") != "video-1" {
		t.Fatalf("quantities %v creatives %+v", w.fees.quantities, w.gateway.creatives)
	}
}

func TestVideoThatMetaCannotProcessFailsAndRefunds(t *testing.T) {
	w := newWorld()
	d := publishableDraft()
	d.Ads[0].Creative = ads.CreativeDraft{Format: ads.FormatVideo, PrimaryText: "x", Media: ads.MediaRef{Kind: ads.MediaVideo, MediaID: "v1"}}
	w.gateway.videoStates = []ads.VideoState{ads.VideoError}
	job, err := publish(t, w, d)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != ads.JobFailed || job.Fee != ads.FeeRefunded || slices.Contains(w.gateway.calls, "campaign") {
		t.Fatalf("job %+v calls %v", job, w.gateway.calls)
	}
}

func TestEveryStepIsSavedInFlightBeforeMetaIsCalled(t *testing.T) {
	w := newWorld()
	if _, err := publish(t, w, publishableDraft()); err != nil {
		t.Fatal(err)
	}
	var inFlight []string
	for _, saved := range w.jobs.log {
		if saved.Progress.InFlight != "" {
			inFlight = append(inFlight, saved.Progress.InFlight)
		}
	}
	want := []string{"media:media-1", "campaign", "adset", "creative:0", "ad:0", "activate"}
	if !slices.Equal(inFlight, want) {
		t.Fatalf("in flight saves %v", inFlight)
	}
}

func TestNoMetaCallWhenTheFeeCannotBeCharged(t *testing.T) {
	w := newWorld()
	w.fees.chargeErr = errors.New("insufficient balance")
	job, err := publish(t, w, publishableDraft())
	if !errors.Is(err, ErrFeeNotCharged) || job.Status != ads.JobFailed {
		t.Fatalf("job %+v err %v", job, err)
	}
	if len(metaWrites(w.gateway.calls)) != 0 {
		t.Fatalf("meta was written without a fee: %v", w.gateway.calls)
	}
}

func TestDefiniteRefusalCleansUpAndRefunds(t *testing.T) {
	w := newWorld()
	w.gateway.failOn = "adset"
	w.gateway.failWith = &ads.RemoteError{Kind: ads.FailureRejected, Code: 100, UserMessage: "Orçamento abaixo do mínimo"}
	job, err := publish(t, w, publishableDraft())
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != ads.JobFailed || job.ErrorMessage != "Orçamento abaixo do mínimo" || job.ErrorCode != "meta_100" {
		t.Fatalf("job %+v", job)
	}
	if !slices.Equal(w.gateway.deleted, []string{"c-1"}) || len(w.fees.refunded) != 1 || job.Fee != ads.FeeRefunded {
		t.Fatalf("deleted %v refunded %v fee %s", w.gateway.deleted, w.fees.refunded, job.Fee)
	}
}

func TestUnconfirmedCreateNeedsReviewAndKeepsTheFee(t *testing.T) {
	w := newWorld()
	w.gateway.failOn = "campaign"
	w.gateway.failWith = &ads.RemoteError{Kind: ads.FailureUnknown, Message: "connection reset"}
	job, err := publish(t, w, publishableDraft())
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != ads.JobNeedsReview || len(w.fees.refunded) != 0 || len(w.gateway.deleted) != 0 {
		t.Fatalf("job %+v refunded %v deleted %v", job, w.fees.refunded, w.gateway.deleted)
	}
}

func TestRateLimitedStepGoesBackToTheQueueForTheReaper(t *testing.T) {
	w := newWorld()
	w.gateway.failOn = "creative"
	w.gateway.failWith = &ads.RemoteError{Kind: ads.FailureRetryable, Code: 4}
	job, err := publish(t, w, publishableDraft())
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != ads.JobQueued || job.Progress.InFlight != "" || job.Progress.AdSetID != "s-1" {
		t.Fatalf("job %+v", job)
	}
	w.gateway.failOn = ""
	if err := w.publisher().Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	resumed, _ := w.jobs.Find(context.Background(), "ws-1", job.ID)
	if resumed.Status != ads.JobPublished || len(w.gateway.campaigns) != 1 {
		t.Fatalf("resumed %+v campaigns %d", resumed, len(w.gateway.campaigns))
	}
}

func TestReaperNeverRetriesACreateThatWasInFlightWhenTheServerStopped(t *testing.T) {
	w := newWorld()
	job := &ads.PublishJob{WorkspaceID: "ws-1", AdAccountID: "acc-1", Draft: publishableDraft(), Status: ads.JobRunning, Fee: ads.FeeCharged}
	_ = w.jobs.Create(context.Background(), job)
	job.Begin(ads.Step{Kind: ads.StepCampaign})
	if err := w.publisher().Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if job.Status != ads.JobNeedsReview || slices.Contains(w.gateway.calls, "campaign") {
		t.Fatalf("job %+v calls %v", job, w.gateway.calls)
	}
}

func TestAddingAnAdToAnExistingAdSetCreatesAndActivatesOnlyTheAd(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.objects.byID["s-1"].DestinationType, w.objects.byID["s-1"].OptimizationGoal = "WHATSAPP", "CONVERSATIONS"
	w.objects.byID["c-1"].Objective = string(ads.ObjectiveEngagement)
	d := ads.AdDraft{AdAccountID: "acc-1", Identity: ads.Identity{PageID: "page-1"}, AdSet: ads.AdSetDraft{ExistingID: "s-1"}, Ads: []ads.AdItem{{Name: "Novo", Creative: imageAd()}}}
	job, err := publish(t, w, d)
	if err != nil || job.Status != ads.JobPublished {
		t.Fatalf("job %+v err %v", job, err)
	}
	want := []string{"image", "creative", "ad", "status:a-1"}
	if got := metaWrites(w.gateway.calls); !slices.Equal(got, want) {
		t.Fatalf("calls %v", got)
	}
}

func TestPreflightRefusesRemoteReferencesMetaDoesNotHave(t *testing.T) {
	cases := map[string]func(*ads.AdDraft){
		"adSet.whatsAppNumber": func(d *ads.AdDraft) { d.AdSet.WhatsAppNumber = "5511900000000" },
		"identity.pageId":      func(d *ads.AdDraft) { d.Identity.PageID = "page-9" },
		"adSet.appId": func(d *ads.AdDraft) {
			d.Campaign.Objective = ads.ObjectiveAppPromotion
			d.AdSet.Destination, d.AdSet.Goal, d.AdSet.AppID, d.AdSet.AppStoreURL = ads.DestinationApp, ads.GoalAppInstalls, "app-9", "https://x.example.com"
		},
		"ads[0].creative.postId": func(d *ads.AdDraft) {
			d.AdSet.Destination, d.AdSet.Goal = ads.DestinationOnPost, ads.GoalPostEngagement
			d.Ads[0].Creative = ads.CreativeDraft{Format: ads.FormatExistingPost, PostID: "page-1_7"}
		},
	}
	for field, mutate := range cases {
		w := newWorld()
		d := publishableDraft()
		mutate(&d)
		_, err := publish(t, w, d)
		var v *ads.ValidationError
		if !errors.As(err, &v) || v.Issues[0].Field != field {
			t.Fatalf("%s: err %v", field, err)
		}
		if len(w.fees.charged) != 0 {
			t.Fatalf("%s: charged for a refused draft", field)
		}
	}
}

func TestPreflightRefusesAnAccountWithoutPaymentMethod(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].HasFunding = false
	if _, err := publish(t, w, publishableDraft()); !errors.Is(err, ads.ErrNoFundingSource) {
		t.Fatalf("err %v", err)
	}
}

func TestPreflightRefusesAnotherWorkspacesAccount(t *testing.T) {
	w := newWorld()
	_, err := w.publisher().Publish(context.Background(), PublishInput{WorkspaceID: "ws-2", Draft: publishableDraft()})
	if !errors.Is(err, ads.ErrAccountNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestPreflightRefusesAGrantThatDoesNotReachTheAccount(t *testing.T) {
	w := newWorld()
	w.grants.byID["grant-1"].GranularScopes = map[string][]string{ads.ScopeAdsManagement: {"999"}}
	if _, err := publish(t, w, publishableDraft()); !errors.Is(err, ads.ErrAccountNeedsReconnect) {
		t.Fatalf("err %v", err)
	}
	if w.accounts.connections["acc-1"] != ads.ConnectionNeedsReconnect {
		t.Fatal("account not flagged for reconnection")
	}
}

func TestPreflightReturnsTheLibraryURLOfEachCreativeMedia(t *testing.T) {
	w := newWorld()
	pre, err := w.publisher().Preflight(context.Background(), "ws-1", publishableDraft())
	if err != nil {
		t.Fatal(err)
	}
	if pre.MediaURLs["media-1"] != "https://cdn/media-1" {
		t.Fatalf("media urls %+v", pre.MediaURLs)
	}
}

func TestAnAccountWithoutPaymentMethodCannotPublishEvenPaused(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].HasFunding = false
	d := publishableDraft()
	d.KeepPaused = true
	if _, err := publish(t, w, d); !errors.Is(err, ads.ErrNoFundingSource) {
		t.Fatalf("published without a payment method: %v", err)
	}
	if len(w.fees.charged) != 0 || len(metaWrites(w.gateway.calls)) != 0 {
		t.Fatalf("charged %v calls %v", w.fees.charged, w.gateway.calls)
	}
}

func pausedPublishedJob(t *testing.T, w *world) *ads.PublishJob {
	t.Helper()
	d := publishableDraft()
	d.KeepPaused = true
	job, err := publish(t, w, d)
	if err != nil || job.Status != ads.JobPublished {
		t.Fatalf("job %+v err %v", job, err)
	}
	return job
}

func TestAPausedPublishSwitchesOnOnceWithOneAction(t *testing.T) {
	w := newWorld()
	job := pausedPublishedJob(t, w)
	w.gateway.calls = nil
	on, err := w.publisher().SwitchOn(context.Background(), "ws-1", job.ID)
	if err != nil || !on.Progress.Activated {
		t.Fatalf("job %+v err %v", on, err)
	}
	if got := metaWrites(w.gateway.calls); !slices.Equal(got, []string{"status:c-1", "status:s-1", "status:a-1"}) {
		t.Fatalf("calls %v", got)
	}
	if _, err := w.publisher().SwitchOn(context.Background(), "ws-1", job.ID); !errors.Is(err, ads.ErrJobNotActivatable) {
		t.Fatalf("switched on twice: %v", err)
	}
}

func TestSwitchOnNeedsAPaymentMethodAndAPausedPublish(t *testing.T) {
	w := newWorld()
	job := pausedPublishedJob(t, w)
	w.accounts.byID["acc-1"].HasFunding = false
	w.gateway.calls = nil
	if _, err := w.publisher().SwitchOn(context.Background(), "ws-1", job.ID); !errors.Is(err, ads.ErrNoFundingSource) || len(metaWrites(w.gateway.calls)) != 0 {
		t.Fatalf("err %v calls %v", err, w.gateway.calls)
	}
	live, err := publish(t, newWorld(), publishableDraft())
	if err != nil {
		t.Fatal(err)
	}
	if err := live.CanSwitchOnLater(); !errors.Is(err, ads.ErrJobNotActivatable) {
		t.Fatalf("a job published switched on can be switched on again: %v", err)
	}
}

func instantFormDraft() ads.AdDraft {
	d := publishableDraft()
	d.Campaign.Objective = ads.ObjectiveLeads
	d.AdSet.Destination, d.AdSet.Goal, d.AdSet.WhatsAppNumber = ads.DestinationInstantForm, ads.GoalLeadGeneration, ""
	creative := imageAd()
	creative.LeadFormID = "form-1"
	d.Ads = []ads.AdItem{{Creative: creative}}
	return d
}

func TestInstantFormAdsWaitForThePageToAcceptTheLeadTerms(t *testing.T) {
	w := newWorld()
	w.gateway.pages[0].LeadTermsAccepted = false
	_, err := w.publisher().Preflight(context.Background(), "ws-1", instantFormDraft())
	requireIssue(t, err, "identity.pageId", "lead_terms_not_accepted")
	if _, err := publish(t, w, instantFormDraft()); err == nil || len(w.fees.charged) != 0 || len(metaWrites(w.gateway.calls)) != 0 {
		t.Fatalf("err %v charged %v calls %v", err, w.fees.charged, w.gateway.calls)
	}
}

func TestAnAudienceFromAnotherAccountIsRefusedBeforeAnyCharge(t *testing.T) {
	w := newWorld()
	w.gateway.audiences = []ads.Audience{{MetaID: "aud-1"}}
	draft := publishableDraft()
	draft.AdSet.Targeting.CustomAudiences = []ads.TargetRef{{ID: "aud-1"}}
	if _, err := w.publisher().Check(context.Background(), "ws-1", draft); err != nil {
		t.Fatalf("an audience of the account must pass: %v", err)
	}
	draft.AdSet.Targeting.ExcludedCustomAudiences = []ads.TargetRef{{ID: "aud-other"}}
	_, err := w.publisher().Check(context.Background(), "ws-1", draft)
	var invalid *ads.ValidationError
	if !errors.As(err, &invalid) || invalid.Issues[0].Field != "adSet.targeting.excludedCustomAudiences" {
		t.Fatalf("got %v", err)
	}
}
