package advertising

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/lead"
)

type fakeTrackedForms struct{ byID map[string]*ads.TrackedForm }

func (f *fakeTrackedForms) Track(_ context.Context, form *ads.TrackedForm) error {
	if existing, ok := f.byID[form.MetaID]; ok && existing.WorkspaceID != form.WorkspaceID {
		return errors.New("tracked elsewhere")
	}
	f.byID[form.MetaID] = form
	return nil
}
func (f *fakeTrackedForms) FindByMetaID(_ context.Context, id string) (*ads.TrackedForm, error) {
	form, ok := f.byID[id]
	if !ok {
		return nil, ads.ErrLeadFormNotFound
	}
	return form, nil
}
func (f *fakeTrackedForms) ListByWorkspace(context.Context, string) ([]*ads.TrackedForm, error) {
	return nil, nil
}
func (f *fakeTrackedForms) ListAll(context.Context, int, int) ([]*ads.TrackedForm, error) {
	var out []*ads.TrackedForm
	for _, form := range f.byID {
		out = append(out, form)
	}
	return out, nil
}
func (f *fakeTrackedForms) MarkPolled(_ context.Context, id string, at time.Time) error {
	f.byID[id].LastPolledAt = &at
	return nil
}

type fakeFormLeads struct {
	saved  map[string]*ads.FormLead
	linked map[string]string
}

func (f *fakeFormLeads) Save(_ context.Context, l *ads.FormLead) (bool, error) {
	if _, ok := f.saved[l.MetaID]; ok {
		return false, nil
	}
	f.saved[l.MetaID] = l
	return true, nil
}
func (f *fakeFormLeads) LinkLead(_ context.Context, metaID, leadID string) error {
	f.linked[metaID] = leadID
	return nil
}
func (f *fakeFormLeads) List(context.Context, ads.FormLeadQuery) ([]*ads.FormLead, int64, error) {
	return nil, 0, nil
}

type fakeCRM struct{ created []string }

func (f *fakeCRM) FindOrCreate(_, number string, _ lead.LeadUpdate) (*lead.Lead, bool, error) {
	f.created = append(f.created, number)
	return &lead.Lead{ID: "lead-" + number}, true, nil
}
func (f *fakeCRM) FindByIDs(string, []string) ([]*lead.Lead, error) { return nil, nil }

func formsWorld() (*world, *FormsUseCase, *fakeTrackedForms, *fakeFormLeads, *fakeCRM) {
	return formsWorldFrom(newWorld())
}

func formsWorldFrom(w *world) (*world, *FormsUseCase, *fakeTrackedForms, *fakeFormLeads, *fakeCRM) {
	forms := &fakeTrackedForms{byID: map[string]*ads.TrackedForm{}}
	leads := &fakeFormLeads{saved: map[string]*ads.FormLead{}, linked: map[string]string{}}
	crm := &fakeCRM{}
	return w, NewFormsUseCase(w.sync, w.gateway, forms, leads, crm, &fakeLeadProfiles{}), forms, leads, crm
}

func TestCreatingAFormTracksItAndSubscribesThePage(t *testing.T) {
	w, uc, forms, _, _ := formsWorld()
	draft := ads.LeadFormDraft{AdAccountID: "acc-1", PageID: "page-1", Name: "Orçamento", PrivacyURL: "https://x.example.com/p", Questions: []ads.FormQuestion{{Type: ads.QuestionPhone}}, ThankYouTitle: "Obrigado", ThankYouURL: "https://x.example.com", ThankYouButtonText: "Visitar site"}
	if _, err := uc.Create(context.Background(), "ws-1", draft); err != nil {
		t.Fatal(err)
	}
	if forms.byID["form-new"].WorkspaceID != "ws-1" || !slices.Contains(w.gateway.calls, "subscribe_leadgen") {
		t.Fatalf("tracked %+v calls %v", forms.byID, w.gateway.calls)
	}
}

func TestPolledLeadsReachTheCRMOnceByPhone(t *testing.T) {
	w, uc, forms, leads, crm := formsWorld()
	forms.byID["f-1"] = &ads.TrackedForm{MetaID: "f-1", WorkspaceID: "ws-1", AdAccountID: "acc-1", PageID: "page-1"}
	w.gateway.leads = []ads.FormLead{
		{MetaID: "l-1", Answers: map[string]string{"full_name": "Ana", "phone_number": "+55 11 98888-7777"}},
		{MetaID: "l-2", Answers: map[string]string{"email": "x@y.com"}},
	}
	imported, err := uc.Sync(context.Background(), "ws-1", "f-1")
	if err != nil || imported != 2 {
		t.Fatalf("imported %d err %v", imported, err)
	}
	if !slices.Equal(crm.created, []string{"5511988887777"}) || leads.linked["l-1"] != "lead-5511988887777" {
		t.Fatalf("crm %v linked %v", crm.created, leads.linked)
	}
	again, _ := uc.Sync(context.Background(), "ws-1", "f-1")
	if again != 0 || len(crm.created) != 1 {
		t.Fatalf("lead imported twice: %d %v", again, crm.created)
	}
	if forms.byID["f-1"].LastPolledAt == nil {
		t.Fatal("poll time not recorded")
	}
}

func TestFormsOfAnotherWorkspaceAreInvisible(t *testing.T) {
	_, uc, forms, _, _ := formsWorld()
	forms.byID["f-1"] = &ads.TrackedForm{MetaID: "f-1", WorkspaceID: "ws-2"}
	if _, err := uc.Sync(context.Background(), "ws-1", "f-1"); !errors.Is(err, ads.ErrLeadFormNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestLeadgenWebhookForAnUntrackedFormIsIgnored(t *testing.T) {
	w, uc, _, _, _ := formsWorld()
	if err := uc.HandleLeadgen(context.Background(), ads.LeadgenEvent{LeadgenID: "l-9", FormID: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("meta called %v", w.gateway.calls)
	}
}

func TestRulesOnlyTargetObjectsOfTheirAccountAndLevel(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	uc := NewRulesUseCase(w.sync, w.gateway)
	rule := ads.AutomatedRule{AdAccountID: "acc-1", Name: "Pausar", Entity: ads.RuleAdSet, ObjectIDs: []string{"c-1"},
		Conditions: []ads.RuleCondition{{Metric: ads.MetricSpent, Operator: ads.OperatorGreaterThan, Value: 50}}, Action: ads.RuleAction{Type: ads.RuleActionPause}}
	_, err := uc.Create(context.Background(), "ws-1", rule)
	requireIssue(t, err, "objectIds", "not_available")
	rule.ObjectIDs = []string{"s-1"}
	if id, err := uc.Create(context.Background(), "ws-1", rule); err != nil || id != "rule-new" {
		t.Fatalf("id %q err %v", id, err)
	}
	if err := uc.SetEnabled(context.Background(), "ws-1", "acc-1", "rule-x", false); !errors.Is(err, ads.ErrRuleNotFound) {
		t.Fatalf("foreign rule toggled: %v", err)
	}
}

func TestSplitTestCellsMustBeAdSetsOfTheAccount(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	uc := NewSplitTestUseCase(w.sync, w.gateway)
	test := ads.SplitTest{AdAccountID: "acc-1", Name: "A x B", Level: ads.TestAdSets,
		Cells:   []ads.TestCell{{Name: "A", ObjectIDs: []string{"s-1"}}, {Name: "B", ObjectIDs: []string{"c-2"}}},
		StartAt: testNow.Add(time.Hour), EndAt: testNow.Add(8 * 24 * time.Hour)}
	_, err := uc.Create(context.Background(), "ws-1", test)
	requireIssue(t, err, "cells[1].objectIds", "not_available")
	test.Cells[1].ObjectIDs = []string{"s-2"}
	if id, err := uc.Create(context.Background(), "ws-1", test); err != nil || id != "study-1" {
		t.Fatalf("id %q err %v", id, err)
	}
}

type fakeSettings struct{ s *ads.ConversionSettings }

func (f *fakeSettings) Get(context.Context, string) (*ads.ConversionSettings, error) {
	if f.s == nil {
		return nil, ads.ErrSettingsNotFound
	}
	return f.s, nil
}
func (f *fakeSettings) Save(_ context.Context, s *ads.ConversionSettings) error {
	f.s = s
	return nil
}
func (f *fakeSettings) ListEnabled(context.Context) ([]*ads.ConversionSettings, error) {
	if f.s == nil || !f.s.Enabled {
		return nil, nil
	}
	return []*ads.ConversionSettings{f.s}, nil
}

type fakeOutbox struct {
	asked    ads.PendingQuery
	released time.Time
	pending  []ads.PendingSignal
	records  []ads.ConversionRecord
	failOn   ads.ConversionStatus
}

func (f *fakeOutbox) Pending(_ context.Context, q ads.PendingQuery) ([]ads.PendingSignal, error) {
	f.asked = q
	return f.pending, nil
}
func (f *fakeOutbox) ReleaseStale(_ context.Context, _ string, claimedBefore time.Time) (int, error) {
	f.released = claimedBefore
	return 0, nil
}
func (f *fakeOutbox) Record(_ context.Context, r ads.ConversionRecord) error {
	if r.Status == f.failOn {
		return errors.New("db down")
	}
	f.records = append(f.records, r)
	return nil
}
func (f *fakeOutbox) Recent(context.Context, string, int) ([]ads.ConversionRecord, error) {
	return f.records, nil
}

type fakeWABAs struct{}

func (fakeWABAs) WABAOf(_ context.Context, _, phoneID string) (string, error) {
	if phoneID != "phone-1" {
		return "", ads.ErrBusinessPhoneNotFound
	}
	return "waba-1", nil
}

func conversionsWorld(pending []ads.PendingSignal) (*world, *ConversionsUseCase, *fakeOutbox) {
	return conversionsWorldFrom(newWorld(), pending)
}

func conversionsWorldFrom(w *world, pending []ads.PendingSignal) (*world, *ConversionsUseCase, *fakeOutbox) {
	settings := &fakeSettings{s: &ads.ConversionSettings{WorkspaceID: "ws-1", AdAccountID: "acc-1", DatasetID: "ds-1", SendLeads: true, SendPurchases: true, Enabled: true}}
	outbox := &fakeOutbox{pending: pending}
	return w, NewConversionsUseCase(w.sync, w.gateway, settings, outbox, fakeWABAs{}), outbox
}

func clickSignal(id string, event ads.DealEvent) ads.PendingSignal {
	return ads.PendingSignal{WorkspaceID: "ws-1", Signal: ads.DealSignal{
		OpportunityID: id, Event: event, At: testNow.Add(-time.Hour), ValueCents: 1000, Currency: "BRL",
		Identity: ads.MessagingIdentity{Channel: ads.ChannelWhatsApp, ClickID: "clid", WABAID: "waba"},
	}}
}

func TestConversionsAreClaimedSentAndRecorded(t *testing.T) {
	w, uc, outbox := conversionsWorld([]ads.PendingSignal{clickSignal("o-1", ads.DealCreated), {WorkspaceID: "ws-1", Signal: ads.DealSignal{OpportunityID: "o-2", Event: ads.DealWon, At: testNow}}})
	if err := uc.DispatchAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.sent) != 1 || w.gateway.sent[0].OpportunityID != "o-1" {
		t.Fatalf("sent %+v", w.gateway.sent)
	}
	statuses := map[string][]ads.ConversionStatus{}
	for _, r := range outbox.records {
		statuses[r.OpportunityID] = append(statuses[r.OpportunityID], r.Status)
	}
	if !slices.Equal(statuses["o-1"], []ads.ConversionStatus{ads.ConversionSending, ads.ConversionSent}) || !slices.Equal(statuses["o-2"], []ads.ConversionStatus{ads.ConversionSkipped}) {
		t.Fatalf("statuses %v", statuses)
	}
}

func TestOnlySwitchedOnEventsAreAskedForAndOthersAreNotMarkedSkipped(t *testing.T) {
	w, uc, outbox := conversionsWorld([]ads.PendingSignal{clickSignal("o-1", ads.DealCreated), clickSignal("o-2", ads.DealWon)})
	settings := uc.settings.(*fakeSettings)
	settings.s.SendPurchases = false
	if err := uc.DispatchAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(outbox.asked.Events, []ads.DealEvent{ads.DealCreated}) || outbox.asked.WorkspaceID != "ws-1" {
		t.Fatalf("asked %+v", outbox.asked)
	}
	for _, r := range outbox.records {
		if r.OpportunityID == "o-2" {
			t.Fatalf("a purchase with its switch off was recorded as %s and would never be sent", r.Status)
		}
	}
	if len(w.gateway.sent) != 1 || w.gateway.sent[0].OpportunityID != "o-1" {
		t.Fatalf("sent %+v", w.gateway.sent)
	}
}

func TestNothingIsAskedForWhenEveryEventIsOff(t *testing.T) {
	_, uc, outbox := conversionsWorld([]ads.PendingSignal{clickSignal("o-1", ads.DealCreated)})
	settings := uc.settings.(*fakeSettings)
	settings.s.SendLeads, settings.s.SendPurchases = false, false
	if err := uc.DispatchAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if outbox.asked.WorkspaceID != "" || len(outbox.records) != 0 {
		t.Fatalf("asked %+v records %+v", outbox.asked, outbox.records)
	}
}

func TestClaimsLeftBehindByACrashAreReleasedBeforeTheNextSend(t *testing.T) {
	_, uc, outbox := conversionsWorld(nil)
	if err := uc.DispatchAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !outbox.released.Equal(testNow.Add(-ads.ConversionClaimTimeout)) {
		t.Fatalf("released claims before %s", outbox.released)
	}
}

func TestAConversionThatCannotBeClaimedIsNeverSent(t *testing.T) {
	w, uc, outbox := conversionsWorld([]ads.PendingSignal{clickSignal("o-1", ads.DealCreated)})
	outbox.failOn = ads.ConversionSending
	if err := uc.DispatchAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.sent) != 0 {
		t.Fatalf("sent without a claim: %+v", w.gateway.sent)
	}
}

func TestConnectingADatasetUsesTheWABAOfTheChosenNumber(t *testing.T) {
	_, uc, _ := conversionsWorld(nil)
	got, err := uc.ConnectDataset(context.Background(), "ws-1", "phone-1")
	if err != nil || got.DatasetID != "ds-1" {
		t.Fatalf("settings %+v err %v", got, err)
	}
}

func TestSplitTestCheckNamesEachVersionAfterItsItemWithoutCreating(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.objects.byID["s-1"].Name, w.objects.byID["s-2"].Name = "Conjunto A", "Conjunto B"
	uc := NewSplitTestUseCase(w.sync, w.gateway)
	test := ads.SplitTest{AdAccountID: "acc-1", Name: "A x B", Level: ads.TestAdSets,
		Cells:   []ads.TestCell{{ObjectIDs: []string{"s-1"}}, {ObjectIDs: []string{"s-2"}}},
		StartAt: testNow.Add(time.Hour), EndAt: testNow.Add(8 * 24 * time.Hour)}
	checked, err := uc.Check(context.Background(), "ws-1", test)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Cells[0].Name != "Conjunto A" || checked.Cells[1].Name != "Conjunto B" || checked.Cells[0].Share+checked.Cells[1].Share != 100 {
		t.Fatalf("checked %+v", checked)
	}
	w.objects.byID["s-2"].Status = ads.StatusArchived
	_, err = uc.Check(context.Background(), "ws-1", test)
	requireIssue(t, err, "cells[1].objectIds", "not_available")
	if slices.Contains(w.gateway.calls, "create_test") {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestRuleCheckReturnsTheTargetedItemsWithoutCreating(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	uc := NewRulesUseCase(w.sync, w.gateway)
	rule := ads.AutomatedRule{AdAccountID: "acc-1", Name: "Pausar", Entity: ads.RuleAdSet, ObjectIDs: []string{"s-1"},
		Conditions: []ads.RuleCondition{{Metric: ads.MetricSpent, Operator: ads.OperatorGreaterThan, Value: 50}}, Action: ads.RuleAction{Type: ads.RuleActionPause}}
	checked, err := uc.Check(context.Background(), "ws-1", rule)
	if err != nil || len(checked.Objects) != 1 || checked.Objects[0].MetaID != "s-1" || checked.Rule.Window != ads.WindowToday {
		t.Fatalf("checked %+v err %v", checked, err)
	}
	rule.ObjectIDs = []string{"c-1"}
	_, err = uc.Check(context.Background(), "ws-1", rule)
	requireIssue(t, err, "objectIds", "not_available")
	if slices.Contains(w.gateway.calls, "create_rule") {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestFormCheckRefusesAPageOutsideTheAccountWithoutCreating(t *testing.T) {
	w, uc, _, _, _ := formsWorld()
	draft := ads.LeadFormDraft{AdAccountID: "acc-1", PageID: "page-1", Name: " Orçamento ", PrivacyURL: "https://x.example.com/p", Questions: []ads.FormQuestion{{Type: ads.QuestionPhone}}, ThankYouTitle: "Obrigado", ThankYouURL: "https://x.example.com", ThankYouButtonText: "Visitar site"}
	checked, err := uc.Check(context.Background(), "ws-1", draft)
	if err != nil || checked.Name != "Orçamento" {
		t.Fatalf("checked %+v err %v", checked, err)
	}
	draft.PageID = "page-x"
	_, err = uc.Check(context.Background(), "ws-1", draft)
	requireIssue(t, err, "pageId", "not_available")
	if slices.Contains(w.gateway.calls, "create_form") {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestConversionCheckFindsThePixelWithoutSaving(t *testing.T) {
	w, uc, _ := conversionsWorld(nil)
	w.gateway.pixels = []ads.Pixel{{MetaID: "px-1", Name: "Site"}, {MetaID: "px-2", Name: "Antigo", Unavailable: true}}
	s := ads.ConversionSettings{AdAccountID: "acc-1", PixelID: "px-1", Enabled: true, SendLeads: true}
	pixel, err := uc.CheckSave(context.Background(), "ws-1", s)
	if err != nil || pixel == nil || pixel.Name != "Site" {
		t.Fatalf("pixel %+v err %v", pixel, err)
	}
	s.PixelID = "px-2"
	_, err = uc.CheckSave(context.Background(), "ws-1", s)
	requireIssue(t, err, "pixelId", "not_available")
	current, _ := uc.Settings(context.Background(), "ws-1")
	if current.PixelID != "" {
		t.Fatalf("the check saved %+v", current)
	}
}

func TestConnectCheckRefusesAnUnknownNumberBeforeMeta(t *testing.T) {
	w, uc, _ := conversionsWorld(nil)
	if account, err := uc.CheckConnectDataset(context.Background(), "ws-1", "phone-1"); err != nil || account.ID != "acc-1" {
		t.Fatalf("account %+v err %v", account, err)
	}
	if _, err := uc.CheckConnectDataset(context.Background(), "ws-1", "phone-x"); !errors.Is(err, ads.ErrBusinessPhoneNotFound) {
		t.Fatalf("an unknown number must be refused, got %v", err)
	}
	if slices.Contains(w.gateway.calls, "dataset") {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}
