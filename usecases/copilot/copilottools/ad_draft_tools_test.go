package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	adsuc "vozko/usecases/advertising"
)

type stubAdDrafts struct {
	created        *advertising.AdDraft
	stored         *adsuc.DraftView
	published      []int
	actor          advertising.Actor
	updated        *advertising.AdDraft
	updatedVersion int
}

func (s *stubAdDrafts) Create(_ context.Context, ws, user string, content advertising.AdDraft) (*adsuc.DraftView, error) {
	s.created = &content
	return &adsuc.DraftView{Draft: &advertising.SavedDraft{ID: "0b7c6a1e-1d2f-4c3b-9a8e-7f6d5c4b3a99", Content: content}, State: advertising.DraftEditing}, nil
}
func (s *stubAdDrafts) List(context.Context, string, string) (*adsuc.DraftList, error) {
	return &adsuc.DraftList{Drafts: []adsuc.DraftView{*s.stored}}, nil
}
func (s *stubAdDrafts) Get(context.Context, string, string) (*adsuc.DraftView, error) {
	return s.stored, nil
}
func (s *stubAdDrafts) CheckEdit(_ context.Context, _, _ string, version int) (*adsuc.DraftView, error) {
	if err := s.stored.Draft.Editable(s.stored.Job, time.Now()); err != nil {
		return nil, err
	}
	if s.stored.Draft.Version != version {
		return nil, advertising.ErrDraftChanged
	}
	return s.stored, nil
}
func (s *stubAdDrafts) Update(_ context.Context, _, _, id string, version int, content advertising.AdDraft) (*adsuc.DraftView, error) {
	s.updated, s.updatedVersion = &content, version
	return &adsuc.DraftView{Draft: &advertising.SavedDraft{ID: id, Version: version + 1, Content: content}, State: advertising.DraftEditing}, nil
}
func (s *stubAdDrafts) Publish(_ context.Context, _, _, _ string, version int, actor advertising.Actor) (*advertising.PublishJob, error) {
	s.published, s.actor = append(s.published, version), actor
	return &advertising.PublishJob{Status: advertising.JobPublished, Progress: advertising.Progress{CampaignID: "c-1", Ads: map[int]string{0: "a-1"}}}, nil
}

type stubAdReadiness struct{}

func (stubAdReadiness) Readiness(context.Context, string, string) (*adsuc.Readiness, error) {
	return &adsuc.Readiness{Checklist: advertising.AccountReadiness{Items: []advertising.ReadinessItem{
		{Key: advertising.ReadyConnection, State: advertising.StateReady, Required: true},
		{Key: advertising.ReadyPaymentMethod, State: advertising.StateMissing, Required: true, Action: advertising.ReadinessAction{Portal: "https://business.facebook.com/billing_hub/payment_settings?asset_id=1"}},
	}}}, nil
}

type stubAdEditor struct {
	checked bool
	edited  *advertising.ObjectEdit
	details map[string]*advertising.ObjectDetail
}

func (e *stubAdEditor) Detail(_ context.Context, _, id string) (*advertising.ObjectDetail, error) {
	if detail, ok := e.details[id]; ok {
		return detail, nil
	}
	level := advertising.LevelAd
	if id == "120200" {
		level = advertising.LevelCampaign
	}
	return &advertising.ObjectDetail{
		Object:   &advertising.Object{MetaID: id, AdAccountID: adAccountUUID, Name: "Anúncio", Level: level},
		Creative: &advertising.CreativeDraft{Format: advertising.FormatImage, PrimaryText: "Antigo"},
	}, nil
}
func (e *stubAdEditor) CheckEdit(ctx context.Context, ws, id string, _ advertising.ObjectEdit) (*advertising.ObjectDetail, error) {
	e.checked = true
	return e.Detail(ctx, ws, id)
}
func (e *stubAdEditor) CheckStatus(context.Context, string, string, bool) (*advertising.Object, error) {
	return nil, errors.New("not used")
}
func (e *stubAdEditor) SetStatus(context.Context, string, string, bool) (*advertising.Object, error) {
	return nil, errors.New("not used")
}
func (e *stubAdEditor) Edit(_ context.Context, _, id string, edit advertising.ObjectEdit) (*advertising.Object, error) {
	e.edited = &edit
	return &advertising.Object{MetaID: id, Name: "Anúncio"}, nil
}

func fullAdDeps() (AdsDeps, *stubAdPublisher, *stubAdDrafts, *stubAdEditor) {
	deps, _, publisher := adDeps()
	drafts, editor := &stubAdDrafts{}, &stubAdEditor{}
	deps.Drafts, deps.Readiness, deps.Editor, deps.Bulk = drafts, stubAdReadiness{}, editor, adsuc.NewBulkUseCase(editor)
	return deps, publisher, drafts, editor
}

func websiteArgs() map[string]interface{} {
	args := createAdArgsMap()
	args["objective"], args["destination"] = "OUTCOME_TRAFFIC", "WEBSITE"
	args["link"], args["display_link"] = "https://vozkoia.com", "vozkoia.com"
	delete(args, "whatsapp_number")
	return args
}

func TestAWebsiteAdTakesTheRecommendedGoalAndShowsTheSite(t *testing.T) {
	deps, publisher, _, _ := fullAdDeps()
	tool := NewCreateAdTool(deps)
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, websiteArgs()); err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	for _, f := range tool.(copilot.Describer).Describe(context.Background(), adContext, websiteArgs()) {
		fields[f.Key] = f.Value
	}
	if fields["destination"] != "Site https://vozkoia.com" || fields["objective"] != "Tráfego" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, websiteArgs()); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	d := publisher.published.Draft
	if d.AdSet.Goal != advertising.GoalLandingPageViews || d.Ads[0].Creative.Link != "https://vozkoia.com" || d.Campaign.Objective != advertising.ObjectiveTraffic {
		t.Fatalf("draft %+v", d)
	}
}

func TestARouteTheObjectiveDoesNotAllowIsRefused(t *testing.T) {
	deps, _, _, _ := fullAdDeps()
	args := createAdArgsMap()
	args["objective"] = "OUTCOME_AWARENESS"
	if err := NewCreateAdTool(deps).(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}

func TestAnUnfundedAccountGetsADraftInsteadOfAPublish(t *testing.T) {
	deps, publisher, drafts, _ := fullAdDeps()
	publisher.unfunded = true
	err := NewCreateAdTool(deps).(copilot.Validator).Validate(context.Background(), adContext, websiteArgs())
	if !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "save_ad_draft") || !strings.Contains(err.Error(), "ad_account_readiness") {
		t.Fatalf("err %v", err)
	}
	save := NewSaveAdDraftTool(deps)
	if err := save.(copilot.Validator).Validate(context.Background(), adContext, websiteArgs()); err != nil {
		t.Fatalf("a draft must be allowed without a payment method: %v", err)
	}
	result := save.Execute(context.Background(), adContext, websiteArgs())
	if result.Status != copilot.StatusOK || drafts.created == nil || result.Card == nil || publisher.published != nil {
		t.Fatalf("result %+v published %+v", result, publisher.published)
	}
}

func TestTheEndDateEndsAtMidnightInTheAccountTimezone(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	args["end_date"] = "2026-10-31"
	if result := NewSaveAdDraftTool(deps).Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	want := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	if end := drafts.created.AdSet.EndAt; end == nil || !end.Equal(want) {
		t.Fatalf("end %v", end)
	}
}

func TestInterestsGoIntoTheAudience(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	args["interests"] = []interface{}{"6003:Marketing digital"}
	NewSaveAdDraftTool(deps).Execute(context.Background(), adContext, args)
	if got := drafts.created.AdSet.Targeting.Interests; len(got) != 1 || got[0].ID != "6003" || got[0].Name != "Marketing digital" {
		t.Fatalf("interests %+v", got)
	}
	args["interests"] = []interface{}{"marketing"}
	if err := NewSaveAdDraftTool(deps).(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("an invented interest must be refused, got %v", err)
	}
}

func TestPublishingADraftSendsTheVersionThatWasReviewed(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	account, _ := deps.account(context.Background(), adContext, adAccountUUID)
	var a adDraftArgs
	if err := decodeArgs(websiteArgs(), &a); err != nil {
		t.Fatal(err)
	}
	content, err := a.draft(account)
	if err != nil {
		t.Fatal(err)
	}
	drafts.stored = &adsuc.DraftView{Draft: &advertising.SavedDraft{ID: "0b7c6a1e-1d2f-4c3b-9a8e-7f6d5c4b3a99", Version: 4, Content: content}, State: advertising.DraftEditing}
	args := map[string]interface{}{"draft_id": "0b7c6a1e-1d2f-4c3b-9a8e-7f6d5c4b3a99", "version": 4}
	tool := NewPublishAdDraftTool(deps)
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK || len(drafts.published) != 1 || drafts.published[0] != 4 || drafts.actor != advertising.ActorAssistant {
		t.Fatalf("result %+v versions %v actor %q", result, drafts.published, drafts.actor)
	}
	drafts.stored.Draft.Version = 5
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("a draft changed after the approval must be refused, got %v", err)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusError || len(drafts.published) != 1 {
		t.Fatalf("a changed draft must not be published, result %+v versions %v", result, drafts.published)
	}
	args["version"] = 5
	drafts.stored.Draft.JobID = "job-1"
	drafts.stored.Job = &advertising.PublishJob{Status: advertising.JobPublished}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("a draft already published must be refused, got %v", err)
	}
	drafts.stored.Job = &advertising.PublishJob{Status: advertising.JobRunning}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("a draft being published must be refused, got %v", err)
	}
}

func TestReadinessShowsTheChecklistCardWithoutLettingEloWriteLinks(t *testing.T) {
	deps, _, _, _ := fullAdDeps()
	result := NewAdAccountReadinessTool(deps).Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID})
	data := result.Data.(map[string]interface{})
	items := data["items"].([]map[string]interface{})
	if data["ready_to_publish"] != false || items[1]["solved_at_meta"] != true || result.Card == nil || result.Card.Kind != copilot.ActionAdReadiness || result.Card.AdAccountID != adAccountUUID {
		t.Fatalf("result %+v", result)
	}
}

func TestEditAdTextChecksWithMetaBeforeChanging(t *testing.T) {
	deps, _, _, editor := fullAdDeps()
	tool := NewEditAdTextTool(deps)
	args := map[string]interface{}{"meta_id": "120300", "field": "primaryText", "value": "Novo texto"}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); err != nil || !editor.checked {
		t.Fatalf("err %v checked %v", err, editor.checked)
	}
	if fields := fieldsOf(tool.(copilot.Describer).Describe(context.Background(), adContext, args)); fields["item"] != "Anúncio" || fields["field"] != "texto principal" || fields["change"] != "passa a ser \"Novo texto\"" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK || editor.edited.Creative.PrimaryText != "Novo texto" {
		t.Fatalf("result %+v edit %+v", result, editor.edited)
	}
	campaignText := map[string]interface{}{"meta_id": "120200", "field": "headline", "value": "x"}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, campaignText); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("ad text on a campaign must be refused, got %v", err)
	}
}

type stubAdForms struct{ created *advertising.LeadFormDraft }

func (f *stubAdForms) List(context.Context, string, string, string) ([]advertising.LeadForm, error) {
	return nil, nil
}
func (f *stubAdForms) Check(_ context.Context, _ string, d advertising.LeadFormDraft) (advertising.LeadFormDraft, error) {
	d.Normalize()
	return d, d.Validate()
}
func (f *stubAdForms) Create(_ context.Context, _ string, d advertising.LeadFormDraft) (*advertising.LeadForm, error) {
	f.created = &d
	return &advertising.LeadForm{MetaID: "777", Name: d.Name}, nil
}

func leadFormArgs() map[string]interface{} {
	return map[string]interface{}{
		"ad_account_id": adAccountUUID, "page_id": "1001", "name": "Cadastro Vozko",
		"questions": []interface{}{"FULL_NAME", "EMAIL", "PHONE"}, "intro_title": "Conheça a Vozko", "intro_text": "Receba uma demonstração.",
		"privacy_url": "https://vozkoia.com/privacidade", "thank_you_title": "Obrigado!", "thank_you_url": "https://vozkoia.com",
		"thank_you_button_text": "Ver o site",
	}
}

func TestALeadFormIsCheckedBeforeItIsCreated(t *testing.T) {
	deps, _, _, _ := fullAdDeps()
	forms := &stubAdForms{}
	deps.Forms = forms
	tool := NewCreateLeadFormTool(deps)
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, leadFormArgs()); err != nil {
		t.Fatal(err)
	}
	result := tool.Execute(context.Background(), adContext, leadFormArgs())
	if result.Status != copilot.StatusOK || forms.created == nil || len(forms.created.Questions) != 3 || forms.created.Intro.Content[0] != "Receba uma demonstração." {
		t.Fatalf("result %+v created %+v", result, forms.created)
	}
	bad := leadFormArgs()
	bad["questions"] = []interface{}{"CPF"}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, bad); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("an unknown question must be refused, got %v", err)
	}
	noPrivacy := leadFormArgs()
	noPrivacy["privacy_url"] = "vozkoia.com"
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, noPrivacy); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("a privacy link that is not https must be refused, got %v", err)
	}
}

func TestAQueuedPublishTellsEloItContinuesInTheBackground(t *testing.T) {
	res := publishResult(&advertising.PublishJob{Status: advertising.JobQueued})
	data := res.Data.(map[string]interface{})
	if res.Status != copilot.StatusOK || data["publish_status"] != string(advertising.JobQueued) || data["message"] != publishInProgress {
		t.Fatalf("result %+v", res)
	}
	done := publishResult(&advertising.PublishJob{Status: advertising.JobPublished}).Data.(map[string]interface{})
	if _, said := done["message"]; said {
		t.Fatalf("a finished publish carried the in progress note: %v", done)
	}
}
