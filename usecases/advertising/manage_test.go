package advertising

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

func TestTurningOnAnAdThatCannotBePaidForIsRefused(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.accounts.byID["acc-1"].HasFunding = false
	if _, err := w.manager().SetStatus(context.Background(), "ws-1", "c-1", true); !errors.Is(err, ads.ErrNoFundingSource) {
		t.Fatalf("err %v", err)
	}
	if _, err := w.manager().SetStatus(context.Background(), "ws-1", "c-1", false); err != nil {
		t.Fatalf("pausing refused: %v", err)
	}
	if w.gateway.statuses["c-1"] != ads.StatusPaused {
		t.Fatalf("statuses %v", w.gateway.statuses)
	}
}

func TestToggleRefreshesTheStructureSoChildrenShowTheParentOff(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.gateway.objects[ads.LevelCampaign] = []*ads.Object{{MetaID: "c-1", Status: ads.StatusPaused, EffectiveStatus: ads.EffectivePaused}}
	w.gateway.objects[ads.LevelAd] = []*ads.Object{{MetaID: "a-1", CampaignMetaID: "c-1", AdSetMetaID: "s-1", Status: ads.StatusActive, EffectiveStatus: ads.EffectiveCampaignPaused}}
	got, err := w.manager().SetStatus(context.Background(), "ws-1", "c-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ads.StatusPaused || w.objects.byID["a-1"].EffectiveStatus != ads.EffectiveCampaignPaused {
		t.Fatalf("campaign %+v ad %+v", got, w.objects.byID["a-1"])
	}
}

func TestObjectsOfAnotherWorkspaceCannotBeToggled(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	if _, err := w.manager().SetStatus(context.Background(), "ws-2", "c-1", false); !errors.Is(err, ads.ErrObjectNotFound) {
		t.Fatalf("err %v", err)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("meta called %v", w.gateway.calls)
	}
}

func adSetWithBudget(w *world) *ads.Object {
	adSet := w.objects.byID["s-1"]
	adSet.DailyBudget = 2000
	w.gateway.detail = &ads.ObjectDetail{Budget: adSet.Budget(), Identity: ads.Identity{PageID: "page-1"}}
	return adSet
}

func TestBudgetChangeKeepsItsKindAndRespectsMetasHourlyLimit(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	adSet := adSetWithBudget(w)
	for i := 0; i < 4; i++ {
		adSet.BudgetChanges = append(adSet.BudgetChanges, testNow.Add(-time.Duration(i)*time.Minute))
	}
	_, err := w.manager().SetBudget(context.Background(), "ws-1", "s-1", 3000)
	requireIssue(t, err, "budget", "too_many_changes")
	adSet.BudgetChanges = nil
	if _, err := w.manager().SetBudget(context.Background(), "ws-1", "s-1", 3000); err != nil {
		t.Fatal(err)
	}
	if spec := w.gateway.edits["s-1"]; spec.Budget == nil || *spec.Budget != (ads.Budget{Kind: ads.BudgetDaily, Amount: 3000}) {
		t.Fatalf("edit %+v", spec)
	}
	if len(w.objects.byID["s-1"].BudgetChanges) != 1 {
		t.Fatal("budget change not recorded")
	}
}

func TestCreativeSwapCreatesANewCreativeAndPointsTheAdAtIt(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.objects.byID["a-1"].DestinationType = "WHATSAPP"
	w.objects.byID["s-1"].DestinationType = "WHATSAPP"
	w.gateway.detail = &ads.ObjectDetail{Identity: ads.Identity{PageID: "page-1"}}
	creative := imageAd()
	if _, err := w.manager().Edit(context.Background(), "ws-1", "a-1", ads.ObjectEdit{Creative: &creative}); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.creatives) != 1 || w.gateway.creatives[0].CallToAction != ads.CTAWhatsAppMessage || w.gateway.edits["a-1"].CreativeID != "cr-1" {
		t.Fatalf("creatives %+v edits %+v", w.gateway.creatives, w.gateway.edits)
	}
}

func TestEditValidationNeverReachesMeta(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	adSetWithBudget(w)
	name := ""
	_, err := w.manager().Edit(context.Background(), "ws-1", "s-1", ads.ObjectEdit{Name: &name})
	requireIssue(t, err, "name", "required")
	if slices.ContainsFunc(w.gateway.calls, func(c string) bool { return c == "update:s-1" }) {
		t.Fatal("invalid edit sent to meta")
	}
}

func TestCopyOnlyIntoAParentOfTheRightLevelInTheSameAccount(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	if _, err := w.manager().Copy(context.Background(), "ws-1", "a-1", ads.CopyRequest{ParentID: "c-2"}); err == nil {
		t.Fatal("ad copied into a campaign")
	}
	id, err := w.manager().Copy(context.Background(), "ws-1", "a-1", ads.CopyRequest{ParentID: "s-2"})
	if err != nil || id != "a-1-copy" {
		t.Fatalf("copy %q %v", id, err)
	}
}

func TestDeleteAndArchiveUseTheRightMetaCall(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	if _, err := w.manager().Lifecycle(context.Background(), "ws-1", "a-1", ads.LifecycleArchive); err != nil {
		t.Fatal(err)
	}
	if _, err := w.manager().Lifecycle(context.Background(), "ws-1", "a-2", ads.LifecycleDelete); err != nil {
		t.Fatal(err)
	}
	if w.gateway.statuses["a-1"] != ads.StatusArchived || !slices.Contains(w.gateway.deleted, "a-2") {
		t.Fatalf("statuses %v deleted %v", w.gateway.statuses, w.gateway.deleted)
	}
}

func TestSpendCapMustStayAboveWhatWasSpent(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].Billing = ads.BillingPostpaid
	w.accounts.byID["acc-1"].AmountSpent = 50_000
	low := int64(40_000)
	if _, err := w.manager().SetSpendCap(context.Background(), "ws-1", "acc-1", &low); !errors.Is(err, ads.ErrInvalidBudget) {
		t.Fatalf("err %v", err)
	}
	if _, err := w.manager().SetSpendCap(context.Background(), "ws-1", "acc-1", nil); err != nil || !slices.Contains(w.gateway.calls, "remove_spend_cap") {
		t.Fatalf("remove %v %v", err, w.gateway.calls)
	}
}

func TestReauthErrorFlagsTheAccountForReconnection(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.gateway.failOn = "status:c-1"
	w.gateway.failWith = &ads.RemoteError{Kind: ads.FailureReauth, Code: 190}
	if _, err := w.manager().SetStatus(context.Background(), "ws-1", "c-1", false); err == nil {
		t.Fatal("expected an error")
	}
	if w.accounts.connections["acc-1"] != ads.ConnectionNeedsReconnect {
		t.Fatal("account not flagged")
	}
}

func requireIssue(t *testing.T, err error, field, code string) {
	t.Helper()
	var v *ads.ValidationError
	if !errors.As(err, &v) || !slices.Contains(v.Issues, ads.FieldIssue{Field: field, Code: code}) {
		t.Fatalf("got %v, want %s:%s", err, field, code)
	}
}

func TestCopyAndLifecycleChecksRefuseWithoutTouchingMeta(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.objects.byID["a-2"].Status = ads.StatusArchived
	if _, err := w.manager().CheckCopy(context.Background(), "ws-1", "a-1", ads.CopyRequest{ParentID: "c-2"}); err == nil {
		t.Fatal("copy into a campaign passed the check")
	}
	if _, err := w.manager().CheckLifecycle(context.Background(), "ws-1", "a-2", ads.LifecycleArchive); err == nil {
		t.Fatal("archiving an archived ad passed the check")
	}
	if object, err := w.manager().CheckCopy(context.Background(), "ws-1", "a-1", ads.CopyRequest{ParentID: "s-2"}); err != nil || object.MetaID != "a-1" {
		t.Fatalf("object %+v err %v", object, err)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("meta called %v", w.gateway.calls)
	}
}

func TestDetailGivesTheAddressOfEachMediaOfAPublishedCreative(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.gateway.detail = &ads.ObjectDetail{Creative: &ads.CreativeDraft{
		Format: ads.FormatCarousel,
		Cards:  []ads.CarouselCard{{Media: ads.MetaMediaRef(ads.MediaImage, "h1")}, {Media: ads.MetaMediaRef(ads.MediaVideo, "v1")}},
	}}
	w.gateway.metaMedia = map[string]string{"meta:h1": "https://cdn.meta/h1.png", "meta:v1": "https://cdn.meta/v1.jpg"}
	detail, err := w.manager().Detail(context.Background(), "ws-1", "a-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.MediaURLs["meta:h1"] != "https://cdn.meta/h1.png" || detail.MediaURLs["meta:v1"] != "https://cdn.meta/v1.jpg" {
		t.Fatalf("media urls %+v", detail.MediaURLs)
	}
}

func TestDetailWithoutMetaMediaDoesNotAskMetaForAddresses(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.gateway.detail = &ads.ObjectDetail{}
	if _, err := w.manager().Detail(context.Background(), "ws-1", "a-1"); err != nil {
		t.Fatal(err)
	}
	for _, call := range w.gateway.calls {
		if call == "meta_media" {
			t.Fatal("asked meta for media addresses with nothing to resolve")
		}
	}
}

func TestCheckingACreativeSwapGivesTheAddressOfEveryProposedMedia(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.gateway.detail = &ads.ObjectDetail{Identity: ads.Identity{PageID: "page-1"}}
	w.gateway.metaMedia = map[string]string{"meta:h1": "https://cdn.meta/h1.png"}
	creative := ads.CreativeDraft{Format: ads.FormatCarousel, PrimaryText: "Oi", Cards: []ads.CarouselCard{
		{Media: ads.MetaMediaRef(ads.MediaImage, "h1")},
		{Media: ads.MediaRef{Kind: ads.MediaImage, MediaID: "media-2"}},
	}}
	checked, err := w.manager().CheckEdit(context.Background(), "ws-1", "a-1", ads.ObjectEdit{Creative: &creative})
	if err != nil {
		t.Fatal(err)
	}
	if checked.MediaURLs["meta:h1"] != "https://cdn.meta/h1.png" || checked.MediaURLs["media-2"] != "https://cdn/media-2" {
		t.Fatalf("media urls %+v", checked.MediaURLs)
	}
}

func TestAPrepaidAccountKeepsTheLimitMetaDerivesFromItsFunds(t *testing.T) {
	for _, kind := range []ads.BillingKind{ads.BillingPrepaid, ads.BillingUnknown} {
		w := newWorld()
		w.accounts.byID["acc-1"].Billing = kind
		cap := int64(90_000)
		for _, amount := range []*int64{&cap, nil} {
			if _, err := w.manager().SetSpendCap(context.Background(), "ws-1", "acc-1", amount); err == nil {
				t.Fatalf("%s: the limit must not change", kind)
			}
		}
		if slices.Contains(w.gateway.calls, "spend_cap") || slices.Contains(w.gateway.calls, "remove_spend_cap") {
			t.Fatalf("%s: Meta was called: %v", kind, w.gateway.calls)
		}
	}
}
