package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
	"vozko/domain/campaignaudience"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type draftCounter struct {
	sendable int
	calls    int
}

func (d *draftCounter) Count(context.Context, *uwc.Campaign) (campaignaudience.Count, error) {
	d.calls++
	n := d.sendable
	return campaignaudience.Count{Sendable: &n}, nil
}

type draftCreator struct {
	created []*uwc.Campaign
}

func (d *draftCreator) Execute(_ context.Context, in *uwc.Campaign, _ uw.DepartmentScope) (*uwc.Campaign, error) {
	stored := *in
	stored.ID = "camp-new"
	stored.Status = campaign.StatusStopped
	d.created = append(d.created, &stored)
	return &stored, nil
}

type draftActions struct {
	acted []string
	err   error
}

func (d *draftActions) Act(_ context.Context, workspaceID string, _ uw.DepartmentScope, campaignID string, action campaign.Action) (*uwc.Campaign, error) {
	d.acted = append(d.acted, workspaceID+"|"+campaignID+"|"+string(action))
	return nil, d.err
}

type draftEstimator struct {
	inputs []uwc.EstimateInput
	groups []uwc.GroupSenders
	err    error
}

func (d *draftEstimator) Draft(_ context.Context, in uwc.EstimateInput, _ uw.DepartmentScope) (*uwc.DraftEstimate, error) {
	d.inputs = append(d.inputs, in)
	if d.err != nil {
		return nil, d.err
	}
	return &uwc.DraftEstimate{Groups: d.groups}, nil
}

func (d *draftEstimator) Remaining(context.Context, *uwc.Campaign) (*uwc.SendEstimate, error) {
	return nil, nil
}

type unofficialDraftFixture struct {
	counter  *draftCounter
	creator  *draftCreator
	actions  *draftActions
	estimate *draftEstimator
	gateway  *fakeGateway
	launch   *DraftLaunch
}

func newUnofficialDraftFixture() unofficialDraftFixture {
	f := unofficialDraftFixture{
		counter: &draftCounter{sendable: 40}, creator: &draftCreator{}, actions: &draftActions{}, estimate: &draftEstimator{},
		gateway: &fakeGateway{instance: &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected}},
	}
	f.launch = NewDraftLaunch(DraftLaunchDeps{Create: f.creator, Actions: f.actions, Estimate: f.estimate, Audience: f.counter, Preflight: NewStartPreflight(f.gateway)})
	return f
}

func leadsDraft() *uwc.Campaign {
	return &uwc.Campaign{WorkspaceID: "ws-1", Name: "Natal", InstanceID: "inst-1", SendDelayMinMS: 9000, SendDelayMaxMS: 15000, Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"oi"}}}
}

func TestUnofficialDraftReviewCountsEstimatesAndChecks(t *testing.T) {
	f := newUnofficialDraftFixture()
	review, err := f.launch.Review(context.Background(), leadsDraft(), uw.DepartmentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if *review.Audience.Sendable != 40 || review.Estimate == nil || review.Report.Blocks() {
		t.Fatalf("review %+v", review)
	}
	if got := f.estimate.inputs[0]; got.Targets != 40 || got.InstanceID != "inst-1" || got.SendDelayMinMS != 9000 {
		t.Fatalf("estimate input %+v", got)
	}
	if len(f.creator.created) != 0 {
		t.Fatal("a review created a campaign")
	}
}

func TestUnofficialDraftReviewBlocksARestrictedNumber(t *testing.T) {
	f := newUnofficialDraftFixture()
	blocked := false
	f.gateway.limits = &uw.Restriction{CanSendNewChats: &blocked}
	review, err := f.launch.Review(context.Background(), leadsDraft(), uw.DepartmentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlock(review.Report, launchcheck.CodeNumberRestricted) {
		t.Fatalf("findings %+v", review.Report.Findings)
	}
}

func TestUnofficialDraftReviewOfGroupsCountsTargetsAndBlocksUnsendableGroups(t *testing.T) {
	f := newUnofficialDraftFixture()
	f.estimate.groups = []uwc.GroupSenders{{JID: "g1", Senders: []uwc.SenderNumber{{InstanceID: "inst-1"}}}, {JID: "g2"}}
	draft := &uwc.Campaign{WorkspaceID: "ws-1", Name: "G", Audience: uwc.AudienceGroups, Targets: []uwc.TargetInput{{GroupJID: "g1"}, {GroupJID: "g2"}}}
	review, err := f.launch.Review(context.Background(), draft, uw.DepartmentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if *review.Audience.Sendable != 2 || f.counter.calls != 0 {
		t.Fatalf("audience %+v counter %d", review.Audience, f.counter.calls)
	}
	if len(f.estimate.inputs[0].GroupJIDs) != 2 || !hasBlock(review.Report, launchcheck.CodeGroupsUnsendable) {
		t.Fatalf("input %+v findings %+v", f.estimate.inputs[0], review.Report.Findings)
	}
}

func TestUnofficialDraftReadyAndScheduleNeverStart(t *testing.T) {
	f := newUnofficialDraftFixture()
	if _, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: leadsDraft(), Launch: campaign.Launch{Mode: campaign.LaunchReady}}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if _, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: leadsDraft(), Launch: campaign.Launch{Mode: campaign.LaunchSchedule, At: at}}); err != nil {
		t.Fatal(err)
	}
	if len(f.actions.acted) != 0 || len(f.creator.created) != 2 {
		t.Fatalf("acted %v created %d", f.actions.acted, len(f.creator.created))
	}
	if !f.creator.created[0].ScheduledStart.IsZero() || !f.creator.created[1].ScheduledStart.Equal(at) {
		t.Fatalf("schedules %v %v", f.creator.created[0].ScheduledStart, f.creator.created[1].ScheduledStart)
	}
}

func TestUnofficialDraftNowStartsThroughTheUsualStart(t *testing.T) {
	f := newUnofficialDraftFixture()
	out, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: leadsDraft(), Launch: campaign.Launch{Mode: campaign.LaunchNow}})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Started || len(f.actions.acted) != 1 || f.actions.acted[0] != "ws-1|camp-new|START" {
		t.Fatalf("out %+v acted %v", out, f.actions.acted)
	}
}

func TestUnofficialDraftNowBlockedCreatesNothing(t *testing.T) {
	f := newUnofficialDraftFixture()
	f.gateway.instance.Status = uw.StatusBanned
	_, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: leadsDraft(), Launch: campaign.Launch{Mode: campaign.LaunchNow}})
	var refused *launchcheck.RefusedError
	if !errors.As(err, &refused) || !hasBlock(refused.Report, launchcheck.CodeNumberUnusable) {
		t.Fatalf("got %v", err)
	}
	if len(f.creator.created) != 0 || len(f.actions.acted) != 0 {
		t.Fatal("a blocked launch created or started a campaign")
	}
}

func TestUnofficialDraftNowWithNoAudienceCreatesNothing(t *testing.T) {
	f := newUnofficialDraftFixture()
	f.counter.sendable = 0
	if _, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: leadsDraft(), Launch: campaign.Launch{Mode: campaign.LaunchNow}}); !errors.Is(err, launchcheck.ErrBlocked) {
		t.Fatalf("got %v", err)
	}
	if len(f.creator.created) != 0 {
		t.Fatal("created an empty campaign")
	}
}

func TestUnofficialDraftNowStartRefusalKeepsTheCampaignReady(t *testing.T) {
	f := newUnofficialDraftFixture()
	f.actions.err = uw.ErrRestrictedByWA
	out, err := f.launch.Launch(context.Background(), DraftLaunchInput{Campaign: leadsDraft(), Launch: campaign.Launch{Mode: campaign.LaunchNow}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || out.Campaign == nil || !errors.Is(out.StartRefusal, uw.ErrRestrictedByWA) {
		t.Fatalf("out %+v", out)
	}
}

func TestUnofficialDraftLaunchFailsClosedWhenNotWired(t *testing.T) {
	if _, err := NewDraftLaunch(DraftLaunchDeps{}).Launch(context.Background(), DraftLaunchInput{Campaign: leadsDraft(), Launch: campaign.Launch{Mode: campaign.LaunchReady}}); !errors.Is(err, ErrDraftLaunchUnavailable) {
		t.Fatalf("got %v", err)
	}
	if err := NewStartPreflight(nil).Check(context.Background(), leadsDraft()); !errors.Is(err, ErrStartPreflightUnavailable) {
		t.Fatalf("preflight got %v", err)
	}
}

func hasBlock(r launchcheck.Report, code launchcheck.Code) bool {
	for _, f := range r.Findings {
		if f.Level == launchcheck.Block && f.Code == code {
			return true
		}
	}
	return false
}
