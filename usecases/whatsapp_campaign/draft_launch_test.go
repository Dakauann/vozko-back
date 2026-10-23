package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
	"vozko/domain/campaignaudience"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

type draftAudience struct {
	sendable int
	err      error
	seen     []*wc.Campaign
}

func (d *draftAudience) Count(_ context.Context, camp *wc.Campaign) (campaignaudience.Count, error) {
	d.seen = append(d.seen, camp)
	n := d.sendable
	return campaignaudience.Count{Sendable: &n}, d.err
}

type draftCreate struct {
	created []*wc.Campaign
	err     error
}

func (d *draftCreate) Execute(_ context.Context, camp *wc.Campaign) (*wc.Campaign, error) {
	if d.err != nil {
		return nil, d.err
	}
	stored := *camp
	stored.ID = "camp-new"
	stored.Status = wc.CampaignStatusStopped
	d.created = append(d.created, &stored)
	return &stored, nil
}

type draftStart struct {
	calls []string
	err   error
}

func (d *draftStart) StartConfirmed(workspaceID string, _ *wd.DepartmentFilter, campaignID, fingerprint string) (*wc.Campaign, error) {
	d.calls = append(d.calls, workspaceID+"|"+campaignID+"|"+fingerprint)
	return nil, d.err
}

type draftFixture struct {
	audience *draftAudience
	create   *draftCreate
	start    *draftStart
	launch   *DraftLaunch
}

func newDraftFixture(deps LaunchCheckDeps) draftFixture {
	f := draftFixture{audience: &draftAudience{sendable: 120}, create: &draftCreate{}, start: &draftStart{}}
	f.launch = NewDraftLaunch(DraftLaunchDeps{Create: f.create, Start: f.start, Check: NewLaunchCheck(deps), Audience: f.audience, Rates: draftRate{rate: 5}})
	return f
}

func draftCampaign() *wc.Campaign {
	return &wc.Campaign{WorkspaceID: "ws-1", Name: "Natal", TemplateID: "tpl-1", BusinessPhoneID: "phone-1"}
}

func TestDraftReviewCountsQuotesAndChecksWithoutACampaign(t *testing.T) {
	f := newDraftFixture(launchDeps())
	review, err := f.launch.Review(context.Background(), draftCampaign())
	if err != nil {
		t.Fatal(err)
	}
	if *review.Audience.Sendable != 120 {
		t.Fatalf("audience %+v", review.Audience)
	}
	if review.Quote == nil || review.Quote.Eligible != 120 || review.Quote.CostMicros != 120*1000 {
		t.Fatalf("quote %+v", review.Quote)
	}
	if len(review.Report.Findings) != 0 {
		t.Fatalf("findings %+v", review.Report.Findings)
	}
	if len(f.create.created) != 0 {
		t.Fatal("a review created a campaign")
	}
}

func TestDraftReviewWithNoAudienceBlocks(t *testing.T) {
	f := newDraftFixture(launchDeps())
	f.audience.sendable = 0
	review, err := f.launch.Review(context.Background(), draftCampaign())
	if err != nil {
		t.Fatal(err)
	}
	if !found(review.Report, launchcheck.Block, launchcheck.CodeAudienceEmpty) {
		t.Fatalf("findings %+v", review.Report.Findings)
	}
}

func TestDraftReviewHasNoLastRun(t *testing.T) {
	deps := launchDeps()
	deps.Failures = nil
	deps.Counts = nil
	f := newDraftFixture(deps)
	if _, err := f.launch.Review(context.Background(), draftCampaign()); err != nil {
		t.Fatalf("a draft review read the stored campaign: %v", err)
	}
}

func TestDraftReadyCreatesAStoppedCampaignWithoutSchedule(t *testing.T) {
	f := newDraftFixture(launchDeps())
	camp := draftCampaign()
	camp.ScheduledStart = time.Now().Add(time.Hour)
	out, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: camp, Launch: campaign.Launch{Mode: campaign.LaunchReady}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || len(f.start.calls) != 0 || len(f.create.created) != 1 || !f.create.created[0].ScheduledStart.IsZero() {
		t.Fatalf("out %+v created %+v", out, f.create.created)
	}
	if len(f.audience.seen) != 0 {
		t.Fatal("ready ran the launch check")
	}
}

func TestDraftScheduleCreatesWithTheTime(t *testing.T) {
	f := newDraftFixture(launchDeps())
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	out, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: draftCampaign(), Launch: campaign.Launch{Mode: campaign.LaunchSchedule, At: at}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || len(f.start.calls) != 0 || !f.create.created[0].ScheduledStart.Equal(at) {
		t.Fatalf("out %+v", out)
	}
}

func TestDraftNowCreatesAndStartsWithTheConfirmation(t *testing.T) {
	deps := launchDeps()
	deps.Exits = launchExits{configured: false}
	f := newDraftFixture(deps)
	review, _ := f.launch.Review(context.Background(), draftCampaign())
	settled := 0
	out, err := f.launch.Launch(context.Background(), DraftLaunchInput{
		Campaign:             draftCampaign(),
		Launch:               campaign.Launch{Mode: campaign.LaunchNow},
		ConfirmedFingerprint: review.Report.Fingerprint(),
		Settle: func(context.Context, *wc.Campaign) []string {
			settled++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Started || settled != 1 || len(f.start.calls) != 1 || f.start.calls[0] != "ws-1|camp-new|"+review.Report.Fingerprint() {
		t.Fatalf("out %+v calls %v", out, f.start.calls)
	}
}

func TestDraftNowBlockedCreatesNothing(t *testing.T) {
	deps := launchDeps()
	deps.Phones = launchPhones{"phone-1": {ID: "phone-1", OwnerWorkspaceID: "ws-1", Status: businessphone.StatusRestricted}}
	f := newDraftFixture(deps)
	_, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: draftCampaign(), Launch: campaign.Launch{Mode: campaign.LaunchNow}})
	var refused *launchcheck.RefusedError
	if !errors.As(err, &refused) || !refused.Report.Blocks() {
		t.Fatalf("got %v", err)
	}
	if len(f.create.created) != 0 || len(f.start.calls) != 0 {
		t.Fatal("a blocked launch created or started a campaign")
	}
}

func TestDraftNowUnconfirmedRiskCreatesNothing(t *testing.T) {
	deps := launchDeps()
	deps.Exits = launchExits{configured: false}
	f := newDraftFixture(deps)
	_, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: draftCampaign(), Launch: campaign.Launch{Mode: campaign.LaunchNow}})
	if !errors.Is(err, launchcheck.ErrUnconfirmed) {
		t.Fatalf("got %v", err)
	}
	if len(f.create.created) != 0 {
		t.Fatal("created after an unconfirmed risk")
	}
}

func TestDraftNowStartRefusalKeepsTheCampaignReady(t *testing.T) {
	f := newDraftFixture(launchDeps())
	f.start.err = wc.ErrCampaignNoSubscription
	out, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: draftCampaign(), Launch: campaign.Launch{Mode: campaign.LaunchNow}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || out.Campaign == nil || !errors.Is(out.StartRefusal, wc.ErrCampaignNoSubscription) {
		t.Fatalf("out %+v", out)
	}
}

func TestDraftNowSettingsFailureDoesNotStart(t *testing.T) {
	f := newDraftFixture(launchDeps())
	out, err := f.launch.Launch(context.Background(), DraftLaunchInput{
		Campaign: draftCampaign(),
		Launch:   campaign.Launch{Mode: campaign.LaunchNow},
		Settle:   func(context.Context, *wc.Campaign) []string { return []string{"deal_automation_not_applied"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || len(f.start.calls) != 0 || !errors.Is(out.StartRefusal, campaign.ErrSettingsNotApplied) {
		t.Fatalf("out %+v", out)
	}
}

func TestDraftLaunchFailsClosedWhenNotWired(t *testing.T) {
	var missing *DraftLaunch
	if _, err := missing.Launch(context.Background(), DraftLaunchInput{Campaign: draftCampaign(), Launch: campaign.Launch{Mode: campaign.LaunchReady}}); !errors.Is(err, ErrDraftLaunchUnavailable) {
		t.Fatalf("got %v", err)
	}
	if _, err := NewDraftLaunch(DraftLaunchDeps{}).Review(context.Background(), draftCampaign()); !errors.Is(err, ErrDraftLaunchUnavailable) {
		t.Fatalf("review got %v", err)
	}
}

type draftRate struct {
	rate float64
	err  error
}

func (d draftRate) CurrentRate() (float64, error) { return d.rate, d.err }

func TestDraftReviewPricesTheQuoteInReaisWithTheWalletRate(t *testing.T) {
	f := newDraftFixture(launchDeps())
	review, err := f.launch.Review(context.Background(), draftCampaign())
	if err != nil {
		t.Fatal(err)
	}
	if review.QuoteBRL == nil || review.QuoteBRL.CostBRL != 0.6 || review.QuoteBRL.UnitPriceBRL != 0.005 {
		t.Fatalf("quote in reais %+v, want 120 sends at R$ 0,005 = R$ 0,60", review.QuoteBRL)
	}
}

func TestDraftReviewFailsClosedWithoutTheWalletRate(t *testing.T) {
	f := newDraftFixture(launchDeps())
	f.launch.d.Rates = draftRate{err: errors.New("no rate")}
	if _, err := f.launch.Review(context.Background(), draftCampaign()); err == nil {
		t.Fatal("a review without the wallet rate must not show a cost in another currency")
	}
}
