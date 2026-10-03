package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	adsuc "vozko/usecases/advertising"
)

const (
	stageUUID      = "3f1b2c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	savedAudUUID   = "9a8b7c6d-5e4f-4a3b-9c2d-1e0f9a8b7c6d"
	sourceAudience = "23850001"
)

type stubAudiences struct {
	terms        bool
	audiences    []advertising.Audience
	saved        []*advertising.SavedAudience
	customerList *advertising.CustomerListDraft
	lookalike    *advertising.LookalikeDraft
	savedNew     *advertising.SavedAudience
	deleted      []string
	checked      *advertising.CustomerListDraft
	matching     int
}

func (s *stubAudiences) List(context.Context, string, string) (*adsuc.AudienceList, error) {
	return &adsuc.AudienceList{TermsAccepted: s.terms, Audiences: s.audiences}, nil
}
func (s *stubAudiences) CheckCustomerList(_ context.Context, _ string, d advertising.CustomerListDraft) (*adsuc.CustomerListResult, error) {
	s.checked = &d
	d.Name = strings.TrimSpace(d.Name)
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if !s.terms {
		return nil, advertising.ErrAudienceTermsNotAccepted
	}
	if s.matching == 0 {
		return nil, advertising.ErrNoCustomersMatched
	}
	return &adsuc.CustomerListResult{Audience: advertising.Audience{Name: d.Name}, Matched: s.matching}, nil
}
func (s *stubAudiences) CheckLookalike(_ context.Context, _ string, d advertising.LookalikeDraft) (advertising.LookalikeDraft, *advertising.Audience, error) {
	d.Normalize()
	if err := d.Validate(); err != nil {
		return advertising.LookalikeDraft{}, nil, err
	}
	if !s.terms {
		return advertising.LookalikeDraft{}, nil, advertising.ErrAudienceTermsNotAccepted
	}
	for i := range s.audiences {
		if s.audiences[i].MetaID == d.OriginAudienceID {
			return d, &s.audiences[i], nil
		}
	}
	return advertising.LookalikeDraft{}, nil, advertising.FieldError("originAudienceId", "not_available")
}
func (s *stubAudiences) CreateCustomerList(_ context.Context, _ string, d advertising.CustomerListDraft) (*adsuc.CustomerListResult, error) {
	s.customerList = &d
	return &adsuc.CustomerListResult{Audience: advertising.Audience{MetaID: "23850099", Name: d.Name}, Matched: 40, Skipped: 2}, nil
}
func (s *stubAudiences) CreateLookalike(_ context.Context, _ string, d advertising.LookalikeDraft) (*advertising.Audience, error) {
	s.lookalike = &d
	return &advertising.Audience{MetaID: "23850100", Name: d.Name}, nil
}
func (s *stubAudiences) Delete(_ context.Context, _, _, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}
func (s *stubAudiences) SavedList(context.Context, string) ([]*advertising.SavedAudience, error) {
	return s.saved, nil
}
func (s *stubAudiences) Save(_ context.Context, _, _ string, a advertising.SavedAudience) (*advertising.SavedAudience, error) {
	a.ID = savedAudUUID
	s.savedNew = &a
	return &a, nil
}
func (s *stubAudiences) DeleteSaved(_ context.Context, _, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func growthObjects() map[string]*advertising.Object {
	return map[string]*advertising.Object{
		"120300": {MetaID: "120300", AdAccountID: adAccountUUID, Name: "Conjunto A", Level: advertising.LevelAdSet},
		"120301": {MetaID: "120301", AdAccountID: adAccountUUID, Name: "Conjunto B", Level: advertising.LevelAdSet},
		"120200": {MetaID: "120200", AdAccountID: adAccountUUID, Name: "Campanha", Level: advertising.LevelCampaign},
	}
}

type growthStubs struct {
	audiences   *stubAudiences
	rules       *stubRules
	tests       *stubSplitTests
	conversions *stubConversions
	pixels      *stubPixels
}

func growthTools() (map[string]copilot.Tool, growthStubs) {
	objects := growthObjects()
	pixels := &stubPixels{pixels: []advertising.Pixel{{MetaID: "555", Name: "Pixel do site"}}}
	s := growthStubs{
		audiences: &stubAudiences{terms: true, matching: 42, audiences: []advertising.Audience{
			{MetaID: sourceAudience, Name: "Clientes", Kind: advertising.AudienceCustomerList, ApproxLower: 1000, ApproxUpper: 1200, DeliveryCode: 200},
		}},
		rules:       &stubRules{objects: objects, rules: []advertising.AutomatedRule{{MetaID: "777", Name: "Pausar caro", Action: advertising.RuleAction{Type: advertising.RuleActionPause}}}},
		tests:       &stubSplitTests{objects: objects},
		conversions: &stubConversions{pixels: pixels, settings: advertising.ConversionSettings{SendLeads: true, SendPurchases: true}},
		pixels:      pixels,
	}
	ads := AdsDeps{Accounts: stubAdAccounts{}}
	deps := AdGrowthDeps{
		Audiences: s.audiences, Rules: s.rules, Tests: s.tests,
		Conversions: s.conversions, Pixels: s.pixels, Now: func() time.Time { return adTestClock },
	}
	byName := map[string]copilot.Tool{}
	for _, tool := range AdGrowthTools(deps, ads) {
		byName[tool.Definition().Name] = tool
	}
	return byName, s
}

func growthValidate(tool copilot.Tool, args map[string]interface{}) error {
	return tool.(copilot.Validator).Validate(context.Background(), adContext, args)
}

func growthDescribe(tool copilot.Tool, args map[string]interface{}) map[string]string {
	out := map[string]string{}
	for _, f := range tool.(copilot.Describer).Describe(context.Background(), adContext, args) {
		out[f.Key] = f.Value
	}
	return out
}

func TestEveryWritingGrowthToolGoesThroughApproval(t *testing.T) {
	tools, _ := growthTools()
	writes := []string{
		"create_customer_list_audience", "create_lookalike_audience", "create_saved_audience", "delete_ad_audience",
		"create_ad_rule", "set_ad_rule_status", "delete_ad_rule", "create_ad_test",
		"save_ad_conversion_settings", "connect_ad_dataset", "create_ad_pixel",
	}
	reads := []string{"list_ad_audiences", "list_ad_rules", "ad_rule_history", "list_ad_tests", "get_ad_conversion_settings", "list_ad_pixels", "recent_ad_conversions"}
	if len(tools) != len(writes)+len(reads) {
		t.Fatalf("tools %d", len(tools))
	}
	for _, name := range writes {
		tool := tools[name]
		_, validates := tool.(copilot.Validator)
		_, describes := tool.(copilot.Describer)
		if !tool.Meta().Mutating || !validates || !describes {
			t.Fatalf("%s must be mutating with Validate and Describe", name)
		}
	}
	for _, name := range reads {
		if tools[name].Meta().Mutating {
			t.Fatalf("%s only reads", name)
		}
	}
}

func TestListAudiencesShowsTheReadinessCardWhenTermsAreMissing(t *testing.T) {
	tools, s := growthTools()
	s.audiences.terms = false
	s.audiences.audiences = append(s.audiences.audiences, advertising.Audience{MetaID: "23850002", Name: "Nova", ApproxLower: -1, ApproxUpper: -1})
	result := tools["list_ad_audiences"].Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID})
	if result.Status != copilot.StatusOK || result.Card == nil || result.Card.Kind != copilot.ActionAdReadiness || result.Card.AdAccountID != adAccountUUID {
		t.Fatalf("result %+v", result)
	}
	data := result.Data.(map[string]interface{})
	rows := data["audiences"].([]map[string]interface{})
	if data["terms_accepted"] != false || rows[0]["size_min"] != int64(1000) || rows[1]["size_min"] != nil || rows[0]["audience_id"] != sourceAudience {
		t.Fatalf("data %+v", data)
	}
}

func customerListArgsMap() map[string]interface{} {
	return map[string]interface{}{"ad_account_id": adAccountUUID, "name": "Clientes ganhos", "stage_ids": []interface{}{stageUUID}, "created_after": "2026-01-01"}
}

func TestCustomerListNeedsTheTermsTheUserAcceptsAtMeta(t *testing.T) {
	tools, s := growthTools()
	s.audiences.terms = false
	tool := tools["create_customer_list_audience"]
	err := growthValidate(tool, customerListArgsMap())
	if !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "ad_account_readiness") {
		t.Fatalf("err %v", err)
	}
	result := tool.Execute(context.Background(), adContext, customerListArgsMap())
	if result.Status != copilot.StatusError || result.Card == nil || result.Card.Kind != copilot.ActionAdReadiness || s.audiences.customerList != nil {
		t.Fatalf("result %+v created %+v", result, s.audiences.customerList)
	}
}

func TestCustomerListShowsTheMatchCountAndReturnsOnlyCounts(t *testing.T) {
	tools, s := growthTools()
	tool := tools["create_customer_list_audience"]
	if err := growthValidate(tool, customerListArgsMap()); err != nil {
		t.Fatal(err)
	}
	if fields := growthDescribe(tool, customerListArgsMap()); !strings.HasPrefix(fields["contacts"], "42 contatos") || fields["audience"] != "Clientes ganhos" {
		t.Fatalf("fields %+v", fields)
	}
	fields := s.audiences.checked.CRMFilter.Fields()
	if len(fields) != 2 || fields[0] != crmfilter.FieldStage || fields[1] != crmfilter.FieldCreatedAt {
		t.Fatalf("filter %+v", s.audiences.checked.CRMFilter)
	}
	result := tool.Execute(context.Background(), adContext, customerListArgsMap())
	data := result.Data.(map[string]interface{})
	if result.Status != copilot.StatusOK || data["contacts_sent"] != 40 || data["contacts_without_phone"] != 2 || len(data) != 4 {
		t.Fatalf("result %+v", result)
	}
	if d := s.audiences.customerList; d.Source != advertising.SourceCRM || d.AdAccountID != adAccountUUID || len(d.CRMFilter.Groups) != 2 {
		t.Fatalf("draft %+v", d)
	}
}

func TestCustomerListRefusesAFilterWithNobodyOrABadDate(t *testing.T) {
	tools, s := growthTools()
	tool := tools["create_customer_list_audience"]
	args := customerListArgsMap()
	args["created_after"] = "ontem"
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("bad date err %v", err)
	}
	s.audiences.matching = 0
	if err := growthValidate(tool, customerListArgsMap()); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("empty err %v", err)
	}
}

func TestLookalikeComesFromAnAudienceOfTheAccount(t *testing.T) {
	tools, s := growthTools()
	tool := tools["create_lookalike_audience"]
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "name": "Parecidos", "source_audience_id": "999", "percent": 1}
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	args["source_audience_id"] = sourceAudience
	args["percent"] = 11
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("percent err %v", err)
	}
	args["percent"] = 3
	if fields := growthDescribe(tool, args); fields["source"] != "Clientes" || fields["country"] != "BR" || fields["size"] != "3%" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK || s.audiences.lookalike.OriginAudienceID != sourceAudience {
		t.Fatalf("result %+v", result)
	}
}

func TestSavedAudienceUsesOnlyListedCustomAudiences(t *testing.T) {
	tools, s := growthTools()
	tool := tools["create_saved_audience"]
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "name": "SP 25+", "locations": []interface{}{"country:BR"}, "age_min": 25, "custom_audiences": []interface{}{"404"}}
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	args["custom_audiences"] = []interface{}{sourceAudience}
	result := tool.Execute(context.Background(), adContext, args)
	if result.Status != copilot.StatusOK || s.audiences.savedNew.Targeting.CustomAudiences[0].Name != "Clientes" || s.audiences.savedNew.Targeting.AgeMin != 25 {
		t.Fatalf("result %+v saved %+v", result, s.audiences.savedNew)
	}
}

func TestDeleteAudienceTakesExactlyOneKnownId(t *testing.T) {
	tools, s := growthTools()
	s.audiences.saved = []*advertising.SavedAudience{{ID: savedAudUUID, Name: "SP 25+"}}
	tool := tools["delete_ad_audience"]
	both := map[string]interface{}{"ad_account_id": adAccountUUID, "audience_id": sourceAudience, "saved_audience_id": savedAudUUID}
	if err := growthValidate(tool, both); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("both err %v", err)
	}
	if err := growthValidate(tool, map[string]interface{}{"ad_account_id": adAccountUUID, "audience_id": "404"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("unknown err %v", err)
	}
	saved := map[string]interface{}{"ad_account_id": adAccountUUID, "saved_audience_id": savedAudUUID}
	if fields := growthDescribe(tool, saved); fields["audience"] != "SP 25+" {
		t.Fatalf("fields %+v", fields)
	}
	tool.Execute(context.Background(), adContext, saved)
	tool.Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID, "audience_id": sourceAudience})
	if len(s.audiences.deleted) != 2 || s.audiences.deleted[0] != savedAudUUID || s.audiences.deleted[1] != sourceAudience {
		t.Fatalf("deleted %+v", s.audiences.deleted)
	}
}
