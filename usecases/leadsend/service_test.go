package leadsend_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

func templateParams() leadaction.SendParams {
	return leadaction.SendParams{Name: "Matrículas", BusinessPhoneID: "bp-1", TemplateID: "tpl-1",
		Bindings: []campaign.VariableBinding{{Source: campaign.BindFirstName}, {Source: campaign.BindDistrict}}}
}

func unofficialParams() leadaction.SendParams {
	return leadaction.SendParams{Name: "Matrículas", InstanceID: "inst-1", Message: &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Olá {{1}}"}},
		Bindings: []campaign.VariableBinding{{Source: campaign.BindName}}}
}

func prepare(h *harness, ctx context.Context, action leadaction.Action, p leadaction.SendParams) (*campaign.SendReview, error) {
	return h.svc.Prepare(ctx, PrepareRequest{Actor: requester(), Departments: allDepartments(), Action: action, Params: p, SnapshotID: "snap-1", Base: "base"})
}

func TestTheServiceRefusesToBuildWithoutAPort(t *testing.T) {
	if _, err := NewService(Deps{}); err == nil {
		t.Fatal("a service without ports was built")
	}
}

func TestPrepareCreatesOneStoppedCampaignHoldingExactlyTheReviewedLeads(t *testing.T) {
	h := newHarness(nil)
	ids := h.seed(7)
	h.leads.byID[ids[2]].Name = ""
	h.world.running[ids[4]] = true

	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(h.world.order) != 1 {
		t.Fatalf("campaigns = %d, want 1", len(h.world.order))
	}
	created := h.world.parts[h.world.order[0]].official
	if created.Status != campaign.StatusStopped || created.Source != campaign.SourceLeadSelection || created.IdempotencyKey != "base:1/1" {
		t.Fatalf("created = %+v", created)
	}
	if len(created.PhoneInputs) != 7 || review.Entries != 7 {
		t.Fatalf("inputs %d review entries %d, want 7 each", len(created.PhoneInputs), review.Entries)
	}
	if review.Eligible != 5 || review.Skipped[campaign.SkipMissingVariable] != 1 || review.Skipped[campaign.SkipAlreadyInRunningCampaign] != 1 {
		t.Fatalf("review = %+v", review)
	}
	if review.Quote.Count != 5 || review.Quote.UnitPriceMicros != 62500 || review.Quote.CostMicros != 5*62500 || review.Quote.Currency == "" {
		t.Fatalf("quote = %+v", review.Quote)
	}
	byLead := map[string][]string{}
	for _, in := range created.PhoneInputs {
		byLead[in.LeadID] = in.Variables
	}
	if !reflect.DeepEqual(byLead[ids[0]], []string{"Maria", "Jardim Silveira"}) {
		t.Fatalf("variables = %v", byLead[ids[0]])
	}
	if len(h.snapshots.dropped) != 1 || h.snapshots.dropped[0] != "snap-1" {
		t.Fatalf("the frozen selection was not dropped: %v", h.snapshots.dropped)
	}
	if scope, ok := wd.GetCreationScope(h.create.contexts[0]); !ok || scope.UserID != "user-1" {
		t.Fatalf("the campaign was created without the requester's scope: %+v", scope)
	}
}

func TestTheReviewNamesWhichVariableEachSkippedLeadIsMissing(t *testing.T) {
	h := newHarness(nil)
	ids := h.seed(4)
	h.leads.byID[ids[1]].Name = ""
	h.leads.byID[ids[3]].Name = "João"

	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	created := h.world.parts[h.world.order[0]].official
	byLead := map[string][]campaign.MissingVariable{}
	for _, in := range created.PhoneInputs {
		byLead[in.LeadID] = in.Missing
	}
	firstName, district := campaign.MissingVariable{Slot: 1, Source: campaign.BindFirstName}, campaign.MissingVariable{Slot: 2, Source: campaign.BindDistrict}
	if !reflect.DeepEqual(byLead[ids[1]], []campaign.MissingVariable{firstName, district}) || !reflect.DeepEqual(byLead[ids[3]], []campaign.MissingVariable{district}) || byLead[ids[0]] != nil {
		t.Fatalf("missing by lead = %+v", byLead)
	}
	want := []campaign.MissingVariableCount{{Slot: 1, Source: campaign.BindFirstName, Count: 1}, {Slot: 2, Source: campaign.BindDistrict, Count: 2}}
	if !reflect.DeepEqual(review.MissingVariables, want) || review.Skipped[campaign.SkipMissingVariable] != 2 {
		t.Fatalf("review missing = %+v skipped %v", review.MissingVariables, review.Skipped)
	}
}

func TestTheReviewStatesTheCooldownDaysTheEntriesRecorded(t *testing.T) {
	h := newHarness(nil)
	h.seed(3)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if review.CooldownDays != 0 {
		t.Fatalf("cooldown days %d with nobody in cooldown", review.CooldownDays)
	}
	part := review.Parts[0].CampaignID
	h.world.parts[part].official.PhoneInputs[0].Skip = campaign.SkipCooldown
	h.world.cooldownDays[part] = 30
	req := SendRequest{Actor: requester(), Departments: allDepartments(), Channel: campaign.ChannelOfficial, CampaignIDs: []string{part}}
	review, err = h.svc.Review(context.Background(), req)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if review.Skipped[campaign.SkipCooldown] != 1 || review.CooldownDays != 30 {
		t.Fatalf("review = skipped %v cooldown days %d, want the 30 the entries recorded", review.Skipped, review.CooldownDays)
	}
	if err := h.svc.Cancel(context.Background(), req); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func TestPrepareReadsTheSnapshotInChunksOfFiveThousand(t *testing.T) {
	h := newHarness(nil)
	h.seed(LeadChunk + 3)
	params := templateParams()
	params.TemplateID = "tpl-literal"
	params.Bindings = []campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "2027"}}
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if h.snapshots.pages != 2 || review.Entries != LeadChunk+3 {
		t.Fatalf("pages %d entries %d", h.snapshots.pages, review.Entries)
	}
	if h.leads.calls != 0 || h.contacts.calls != 0 {
		t.Fatal("a literal-only send read the leads")
	}
}

func TestARetriedPreparationReturnsTheSameCampaign(t *testing.T) {
	h := newHarness(nil)
	h.seed(3)
	first, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	again, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(h.world.parts) != 1 || first.Parts[0].CampaignID != again.Parts[0].CampaignID {
		t.Fatalf("retry made another campaign: %v vs %v", first.Parts, again.Parts)
	}
	existing, err := h.svc.Existing(context.Background(), requester(), allDepartments(), leadaction.ActionSendTemplate, "base")
	if err != nil || existing == nil || existing.Parts[0].CampaignID != first.Parts[0].CampaignID {
		t.Fatalf("Existing = %+v, %v", existing, err)
	}
	missing, err := h.svc.Existing(context.Background(), requester(), allDepartments(), leadaction.ActionSendTemplate, "other")
	if err != nil || missing != nil {
		t.Fatalf("an unknown send = %+v, %v", missing, err)
	}
}

func TestAWorkerWithoutCreationScopeCreatesNothing(t *testing.T) {
	h := newHarness(nil)
	h.seed(3)
	for name, ctx := range map[string]context.Context{
		"no scope":        context.Background(),
		"another person":  wd.WithCreationScope(context.Background(), wd.CreationScope{UserID: "user-2"}),
		"an empty person": wd.WithCreationScope(context.Background(), wd.CreationScope{}),
	} {
		if _, err := prepare(h, ctx, leadaction.ActionSendTemplate, templateParams()); !errors.Is(err, campaign.ErrCreationScopeMissing) {
			t.Fatalf("%s: Prepare = %v, want ErrCreationScopeMissing", name, err)
		}
	}
	if len(h.create.contexts) != 0 {
		t.Fatal("a campaign creation was attempted without the creation scope")
	}
}

func TestAWorkspaceWithDepartmentsNeedsTheDepartmentOfTheSend(t *testing.T) {
	h := newHarness(func(d *Deps) { d.Departments = fakeDepartments{list: []wd.Department{{ID: "dept-1"}}} })
	h.seed(2)
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams()); !errors.Is(err, campaign.ErrDepartmentRequired) {
		t.Fatalf("Prepare = %v, want ErrDepartmentRequired", err)
	}
	params := templateParams()
	params.DepartmentID = "dept-1"
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params); err != nil {
		t.Fatalf("Prepare with the department: %v", err)
	}
	if scope, _ := wd.GetCreationScope(h.create.contexts[0]); scope.RequestedDepartmentID != "dept-1" {
		t.Fatalf("the campaign was created for department %q", scope.RequestedDepartmentID)
	}
}

func TestTemplatesThatCannotGoToASelectionAreRefused(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)
	params := templateParams()
	params.TemplateID = "tpl-header"
	params.Bindings = params.Bindings[:1]
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params); !errors.Is(err, campaign.ErrHeaderVariableUnsupported) {
		t.Fatalf("header variable = %v", err)
	}
	params = templateParams()
	params.Bindings = []campaign.VariableBinding{{Source: campaign.BindFirstName}, {Source: campaign.CustomBinding("classificacao")}}
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params); !errors.Is(err, campaign.ErrBindingSensitive) {
		t.Fatalf("sensitive binding = %v", err)
	}
	params = templateParams()
	params.TemplateID = "tpl-missing"
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params); err == nil {
		t.Fatal("a template the workspace cannot use was accepted")
	}
}

func TestASelectionOverTheCampaignCapIsRefusedOrSplit(t *testing.T) {
	h := newHarness(nil)
	h.seed(campaign.MaxEntries + 2)
	params := templateParams()
	params.TemplateID = "tpl-literal"
	params.Bindings = []campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "2027"}}
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params); !errors.Is(err, campaign.ErrSelectionOverCampaignCap) {
		t.Fatalf("Prepare = %v, want ErrSelectionOverCampaignCap", err)
	}
	if len(h.world.parts) != 0 {
		t.Fatal("a refused selection created a campaign")
	}
	params.Split = true
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params)
	if err != nil {
		t.Fatalf("split Prepare: %v", err)
	}
	if len(review.Parts) != 2 || review.Entries != campaign.MaxEntries+2 || review.Parts[0].Name != "Matrículas (1/2)" {
		t.Fatalf("split review = %+v", review.Parts)
	}
}

func TestTheUnofficialPreparationUsesTheConnectedNumber(t *testing.T) {
	h := newHarness(nil)
	h.seed(4)
	review, err := prepare(h, scoped(), leadaction.ActionSendUnofficial, unofficialParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if review.Channel != campaign.ChannelUnofficial || review.Quote.DailyCap != 250 || review.Quote.EstimatedDays != 1 || review.Quote.Fits != 4 {
		t.Fatalf("review = %+v", review)
	}
	for _, p := range h.world.parts {
		if p.unofficial == nil || p.unofficial.CreatedByID != "user-1" || len(p.unofficial.Targets) != 4 {
			t.Fatalf("created = %+v", p.unofficial)
		}
	}
}

func TestQuoteOfAPreview(t *testing.T) {
	h := newHarness(func(d *Deps) {
		d.Balances = fakeBalances{micros: 625_000}
		d.Caps = fakeCaps{usage: &balance.SendCapUsage{Cap: balance.MonthlySendCap{Limit: 100}, Used: 95}}
	})
	q, err := h.svc.Quote(scoped(), requester(), leadaction.ActionSendTemplate, templateParams(), 200_000)
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if q.Parts != 2 || !q.SplitRequired || q.Fits != 5 || q.Refusal != "over_cap" || q.CapRemaining == nil || *q.CapRemaining != 5 {
		t.Fatalf("quote = %+v", q)
	}
	u, err := h.svc.Quote(scoped(), requester(), leadaction.ActionSendUnofficial, unofficialParams(), 1122)
	if err != nil || u.DailyCap != 250 || u.EstimatedDays != 5 || u.Refusal != "" {
		t.Fatalf("unofficial quote = %+v, %v", u, err)
	}
}

func startRequest(review *campaign.SendReview, firstN int) SendRequest {
	ids := make([]string, 0, len(review.Parts))
	for _, p := range review.Parts {
		ids = append(ids, p.CampaignID)
	}
	return SendRequest{Actor: requester(), Departments: allDepartments(), Channel: review.Channel, CampaignIDs: ids, FirstN: firstN}
}

func TestStartRefusesWhatTheBalanceCannotPayUnlessTheFirstNFit(t *testing.T) {
	h := newHarness(func(d *Deps) { d.Balances = fakeBalances{micros: 3 * 62500} })
	h.seed(5)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	_, err = h.svc.Start(context.Background(), startRequest(review, 0))
	var refusal *campaign.BudgetRefusal
	if !errors.As(err, &refusal) || !errors.Is(err, campaign.ErrUnaffordable) || refusal.Fits != 3 {
		t.Fatalf("Start = %v, want unaffordable with 3 that fit", err)
	}
	if len(h.start.started) != 0 {
		t.Fatal("an unaffordable send started")
	}
	started, err := h.svc.Start(context.Background(), startRequest(review, 3))
	if err != nil {
		t.Fatalf("Start first 3: %v", err)
	}
	if len(h.start.started) != 1 || h.world.skipped[review.Parts[0].CampaignID] != 2 {
		t.Fatalf("started %v skipped %v", h.start.started, h.world.skipped)
	}
	if !started.Started || started.Skipped[campaign.SkipOverCap] != 2 {
		t.Fatalf("after start = %+v", started)
	}
	again, err := h.svc.Start(context.Background(), startRequest(review, 0))
	if err != nil || len(h.start.started) != 1 || !again.Started {
		t.Fatalf("a second start = %+v, %v (starts %d)", again, err, len(h.start.started))
	}
}

func TestTheUnofficialStartDispatches(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)
	review, err := prepare(h, scoped(), leadaction.ActionSendUnofficial, unofficialParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := h.svc.Start(context.Background(), startRequest(review, 0)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(h.act.started) != 1 {
		t.Fatalf("dispatched %v", h.act.started)
	}
}

func TestCancelDeletesAStoppedSendAndRefusesAStartedOne(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := h.svc.Cancel(context.Background(), startRequest(review, 0)); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(h.world.deleted) != 1 || len(h.cached.forgotten) != 1 || h.cached.forgotten[0] != h.world.deleted[0] {
		t.Fatalf("deleted %v forgotten %v", h.world.deleted, h.cached.forgotten)
	}
	h2 := newHarness(nil)
	h2.seed(2)
	review, _ = prepare(h2, scoped(), leadaction.ActionSendTemplate, templateParams())
	if _, err := h2.svc.Start(context.Background(), startRequest(review, 0)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := h2.svc.Cancel(context.Background(), startRequest(review, 0)); !errors.Is(err, campaign.ErrAlreadyStarted) {
		t.Fatalf("Cancel = %v, want ErrAlreadyStarted", err)
	}
}

func TestACancelThatLosesTheRaceToAStartDeletesNothing(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	h.world.startedBy[review.Parts[0].CampaignID] = true
	if err := h.svc.Cancel(context.Background(), startRequest(review, 0)); !errors.Is(err, campaign.ErrAlreadyStarted) {
		t.Fatalf("Cancel = %v, want ErrAlreadyStarted", err)
	}
	if len(h.world.deleted) != 0 {
		t.Fatalf("a send that started meanwhile was deleted: %v", h.world.deleted)
	}
}

func TestAFirstNInsideTheFirstPartStartsOnlyThatPart(t *testing.T) {
	h := newHarness(nil)
	h.seed(campaign.MaxEntries + 2)
	params := templateParams()
	params.TemplateID, params.Split = "tpl-literal", true
	params.Bindings = []campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "2027"}}
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	started, err := h.svc.Start(context.Background(), startRequest(review, 5))
	if err != nil {
		t.Fatalf("Start first 5 = %v, the send went out and must answer its review", err)
	}
	if len(h.start.started) != 1 || h.start.started[0] != review.Parts[0].CampaignID {
		t.Fatalf("started %v, want only the first part", h.start.started)
	}
	if !started.Started || started.Skipped[campaign.SkipOverCap] != campaign.MaxEntries+2-5 {
		t.Fatalf("after start = %+v", started.Skipped)
	}
}

func TestStartSkipsLeadsThatJoinedARunningCampaignAfterTheReview(t *testing.T) {
	h := newHarness(func(d *Deps) { d.Balances = fakeBalances{micros: 4 * 62500} })
	ids := h.seed(5)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	h.world.lateRunning[ids[0]] = true
	started, err := h.svc.Start(context.Background(), startRequest(review, 0))
	if err != nil {
		t.Fatalf("Start = %v, the lead already in a running campaign must not be counted", err)
	}
	if started.Skipped[campaign.SkipAlreadyInRunningCampaign] != 1 || len(h.start.started) != 1 {
		t.Fatalf("after start = %+v started %v", started.Skipped, h.start.started)
	}
}

func TestTheReviewTallyWaitsForTheAnalyticsGate(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	h.svc.deps.Gate = openGate{busy: true}
	if _, err := h.svc.Review(context.Background(), startRequest(review, 0)); !errors.Is(err, cache.ErrGateBusy) {
		t.Fatalf("Review = %v, want ErrGateBusy", err)
	}
}

func TestReviewRefusesWhatIsNotOneWholeSendFromASelection(t *testing.T) {
	h := newHarness(nil)
	h.seed(campaign.MaxEntries + 2)
	params := templateParams()
	params.TemplateID, params.Split = "tpl-literal", true
	params.Bindings = []campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "2027"}}
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	half := SendRequest{Actor: requester(), Departments: allDepartments(), Channel: campaign.ChannelOfficial, CampaignIDs: []string{review.Parts[0].CampaignID}}
	if _, err := h.svc.Review(context.Background(), half); !errors.Is(err, campaign.ErrSendIncomplete) {
		t.Fatalf("half a send = %v", err)
	}
	plainCampaign := *h.world.parts[review.Parts[0].CampaignID].official
	plainCampaign.ID, plainCampaign.Source = "plain", ""
	h.world.parts["plain"] = &createdPart{official: &plainCampaign}
	plain := SendRequest{Actor: requester(), Departments: allDepartments(), Channel: campaign.ChannelOfficial, CampaignIDs: []string{"plain"}}
	if _, err := h.svc.Review(context.Background(), plain); !errors.Is(err, campaign.ErrNotFromSelection) {
		t.Fatalf("a plain campaign = %v", err)
	}
	if _, err := h.svc.Review(context.Background(), SendRequest{Actor: requester(), Departments: allDepartments(), Channel: campaign.ChannelOfficial}); !errors.Is(err, campaign.ErrSendIncomplete) {
		t.Fatalf("no ids = %v", err)
	}
}

func TestSendRoutesNeedTheSendCapability(t *testing.T) {
	h := newHarness(func(d *Deps) {
		d.Permissions = fakePermissions{denied: map[string]bool{"whatsapp_campaigns:start": true}}
	})
	req := SendRequest{Actor: requester(), Departments: allDepartments(), Channel: campaign.ChannelOfficial, CampaignIDs: []string{"x"}}
	if _, err := h.svc.Review(context.Background(), req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Review = %v", err)
	}
	if _, err := h.svc.Start(context.Background(), req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Start = %v", err)
	}
	if err := h.svc.Cancel(context.Background(), req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Cancel = %v", err)
	}
}

func TestWithoutTheUnofficialChannelUnofficialSendsAreUnavailable(t *testing.T) {
	h := newHarness(func(d *Deps) { d.Unofficial = nil })
	h.seed(2)
	if _, err := prepare(h, scoped(), leadaction.ActionSendUnofficial, unofficialParams()); !errors.Is(err, leadaction.ErrUnavailable) {
		t.Fatalf("Prepare = %v, want ErrUnavailable", err)
	}
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams()); err != nil {
		t.Fatalf("the official channel stopped working: %v", err)
	}
}

func TestStartingASendWhereNobodyCanReceiveIsRefused(t *testing.T) {
	h := newHarness(nil)
	ids := h.seed(2)
	for _, id := range ids {
		h.world.running[id] = true
	}
	review, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := h.svc.Start(context.Background(), startRequest(review, 0)); !errors.Is(err, campaign.ErrNothingEligible) {
		t.Fatalf("Start = %v, want ErrNothingEligible", err)
	}
	if len(h.start.started) != 0 {
		t.Fatal("a send nobody can receive was started")
	}
}
