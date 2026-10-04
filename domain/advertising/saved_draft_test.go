package advertising

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func newTreeDraft() AdDraft {
	return AdDraft{
		Campaign: CampaignDraft{Name: " Nova campanha de Leads ", Objective: ObjectiveLeads, Budget: &Budget{Kind: BudgetDaily, Amount: 2500}},
		AdSet:    AdSetDraft{Name: "Novo conjunto", Destination: DestinationWhatsApp, Goal: GoalConversations},
		Ads:      []AdItem{{Name: "Anúncio A"}, {Name: "Anúncio B"}},
	}
}

func TestNewSavedDraftNormalizesAndTakesTheAccount(t *testing.T) {
	d, err := NewSavedDraft("ws", "acc-1", "u-1", newTreeDraft())
	if err != nil {
		t.Fatal(err)
	}
	if d.AdAccountID != "acc-1" || d.Content.AdAccountID != "acc-1" || d.Content.Campaign.Name != "Nova campanha de Leads" || d.CreatedBy != "u-1" || d.UpdatedBy != "u-1" {
		t.Fatalf("got %+v", d)
	}
}

func TestSavedDraftKeepsIncompleteContentWithListsNeverNull(t *testing.T) {
	d, err := NewSavedDraft("ws", "acc-1", "u-1", AdDraft{})
	if err != nil {
		t.Fatalf("an empty draft must save: %v", err)
	}
	raw, _ := json.Marshal(d.Content)
	if strings.Contains(string(raw), "null") {
		t.Fatalf("the editor reads lists, got %s", raw)
	}
}

func TestSavedDraftRefusesContentBeyondTheBounds(t *testing.T) {
	tooMany := newTreeDraft()
	tooMany.Ads = make([]AdItem, maxAdsPerDraft+1)
	tooLong := newTreeDraft()
	tooLong.AdSet.Name = strings.Repeat("x", maxNameRunes+1)
	for want, content := range map[string]AdDraft{"ads:too_many": tooMany, "adSet.name:too_long": tooLong} {
		_, err := NewSavedDraft("ws", "acc-1", "u-1", content)
		var invalid *ValidationError
		if !errors.As(err, &invalid) || len(invalid.Issues) != 1 || invalid.Issues[0].Field+":"+invalid.Issues[0].Code != want {
			t.Fatalf("want %s, got %v", want, err)
		}
	}
}

func TestDraftStateFollowsTheJob(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	d := &SavedDraft{UpdatedAt: now}
	if d.State(nil, now) != DraftEditing || d.Editable(nil, now) != nil {
		t.Fatal("a draft without a job is editable")
	}
	d.JobID = "j-1"
	cases := map[JobStatus]DraftState{
		JobQueued: DraftPublishing, JobRunning: DraftPublishing, JobPublished: DraftPublished,
		JobFailed: DraftFailed, JobNeedsReview: DraftFailed,
	}
	for status, want := range cases {
		if got := d.State(&PublishJob{Status: status}, now); got != want {
			t.Errorf("%s: got %s want %s", status, got, want)
		}
	}
	if !errors.Is(d.Editable(&PublishJob{Status: JobRunning}, now), ErrDraftPublishing) || d.Editable(&PublishJob{Status: JobFailed}, now) != nil {
		t.Fatal("only a failed job lets the draft change again")
	}
}

func TestAClaimWithoutAJobHoldsTheDraftUntilItTimesOut(t *testing.T) {
	claimed := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	d := &SavedDraft{JobID: "j-1", UpdatedAt: claimed}
	if d.State(nil, claimed.Add(time.Minute)) != DraftPublishing {
		t.Fatal("a fresh claim is a publish in progress")
	}
	if d.State(nil, claimed.Add(unconfirmedClaimTimeout)) != DraftEditing {
		t.Fatal("a claim that never became a job frees the draft")
	}
}

func TestRowsOfANewTree(t *testing.T) {
	d, _ := NewSavedDraft("ws", "acc-1", "u-1", newTreeDraft())
	d.ID = "d-1"
	rows := d.Rows()
	if len(rows) != 4 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].Key != "d-1:campaign" || rows[0].Level != LevelCampaign || rows[0].Budget.Amount != 2500 {
		t.Fatalf("campaign %+v", rows[0])
	}
	if rows[1].Key != "d-1:adset" || rows[1].ParentKey != "d-1:campaign" || rows[1].Destination != DestinationWhatsApp {
		t.Fatalf("ad set %+v", rows[1])
	}
	if rows[3].Key != "d-1:ad:1" || rows[3].ParentKey != "d-1:adset" || rows[3].Name != "Anúncio B" {
		t.Fatalf("ad %+v", rows[3])
	}
}

func TestRowsUnderExistingParents(t *testing.T) {
	content := newTreeDraft()
	content.Campaign.ExistingID = "c-9"
	d, _ := NewSavedDraft("ws", "acc-1", "u-1", content)
	d.ID = "d-2"
	rows := d.Rows()
	if len(rows) != 3 || rows[0].Level != LevelAdSet || rows[0].ParentKey != "" || rows[0].ParentMetaID != "c-9" {
		t.Fatalf("rows %+v", rows)
	}
	content.AdSet.ExistingID = "s-9"
	d, _ = NewSavedDraft("ws", "acc-1", "u-1", content)
	d.ID = "d-3"
	rows = d.Rows()
	if len(rows) != 2 || rows[0].Level != LevelAd || rows[0].ParentMetaID != "s-9" || rows[0].ParentKey != "" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestACopyRenamesTheTopNewLevelAndLeavesTheOriginal(t *testing.T) {
	original, _ := NewSavedDraft("ws", "acc-1", "u-1", newTreeDraft())
	original.ID, original.JobID = "d-1", "j-1"
	copied, err := original.Copy("u-2", "Nova campanha de Leads - Cópia")
	if err != nil {
		t.Fatal(err)
	}
	if copied.ID != "" || copied.JobID != "" || copied.CreatedBy != "u-2" || copied.Content.Campaign.Name != "Nova campanha de Leads - Cópia" {
		t.Fatalf("got %+v", copied)
	}
	if original.Content.Campaign.Name != "Nova campanha de Leads" {
		t.Fatal("the original must not change")
	}
	underAdSet := newTreeDraft()
	underAdSet.Campaign.ExistingID, underAdSet.AdSet.ExistingID = "c-9", "s-9"
	underAdSet.Ads = underAdSet.Ads[:1]
	single, _ := NewSavedDraft("ws", "acc-1", "u-1", underAdSet)
	copied, _ = single.Copy("u-1", "Anúncio A - Cópia")
	if copied.Content.Ads[0].Name != "Anúncio A - Cópia" || single.Content.Ads[0].Name != "Anúncio A" {
		t.Fatalf("got %+v", copied.Content.Ads)
	}
}

func TestAnUnnamedAdUnderAnExistingAdSetListsUnderTheNameItWillPublishWith(t *testing.T) {
	content := AdDraft{AdSet: AdSetDraft{ExistingID: "s-9"}, Ads: []AdItem{{Name: "  "}}}
	d, _ := NewSavedDraft("ws", "acc-1", "u-1", content)
	d.ID = "d-4"
	parents := ExistingParents{
		Campaign: &Object{MetaID: "c-9", Name: "Vozko CRM | Mensagens WhatsApp"},
		AdSet:    &Object{MetaID: "s-9", Name: "BR | 25+", CampaignMetaID: "c-9"},
	}
	rows := d.RowsUnder(parents)
	if len(rows) != 1 || rows[0].Name != "Vozko CRM | Mensagens WhatsApp" {
		t.Fatalf("rows %+v", rows)
	}

	published := d.Content
	published.Adopt(parents)
	published.Normalize()
	if published.Ads[0].Name != rows[0].Name {
		t.Fatalf("listed %q but publishes %q", rows[0].Name, published.Ads[0].Name)
	}
	if d.Content.Campaign.Name != "" {
		t.Fatal("listing must not change the saved draft")
	}
}

func TestUnnamedAdsInANewTreeAreNumberedAfterTheCampaign(t *testing.T) {
	content := newTreeDraft()
	content.Ads = []AdItem{{}, {Name: "Anúncio B"}}
	d, _ := NewSavedDraft("ws", "acc-1", "u-1", content)
	rows := d.Rows()
	if rows[2].Name != "Nova campanha de Leads 1" || rows[3].Name != "Anúncio B" {
		t.Fatalf("rows %+v", rows)
	}
}
