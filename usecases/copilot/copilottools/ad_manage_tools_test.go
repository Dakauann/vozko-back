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

const otherAdAccountUUID = "5b1d2f3a-8c4e-4d6f-9a1b-3c5e7f9a1b2c"

type manageAccounts struct{ account *advertising.AdAccount }

func (m manageAccounts) List(context.Context, string) ([]*advertising.AdAccount, error) {
	return []*advertising.AdAccount{m.account}, nil
}

func adminAdAccount() *advertising.AdAccount {
	return &advertising.AdAccount{
		ID: adAccountUUID, Name: "Loja", Currency: "BRL", Timezone: "America/Sao_Paulo", Connection: advertising.ConnectionConnected,
		Tasks: []string{advertising.TaskManage}, AmountSpent: 120_000, SpendCap: 500_000,
	}
}

type manageEditor struct {
	details map[string]*advertising.ObjectDetail
	checked []string
	edited  map[string]advertising.ObjectEdit
}

func (e *manageEditor) Detail(_ context.Context, _, id string) (*advertising.ObjectDetail, error) {
	detail, ok := e.details[id]
	if !ok {
		return nil, advertising.ErrObjectNotFound
	}
	return detail, nil
}

func (e *manageEditor) CheckEdit(ctx context.Context, ws, id string, edit advertising.ObjectEdit) (*advertising.ObjectDetail, error) {
	detail, err := e.Detail(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	e.checked = append(e.checked, id)
	edit.Normalize()
	if err := edit.Validate(*detail, false, adTestClock); err != nil {
		return nil, err
	}
	return detail, nil
}

func (e *manageEditor) Edit(_ context.Context, _, id string, edit advertising.ObjectEdit) (*advertising.Object, error) {
	e.edited[id] = edit
	return e.details[id].Object, nil
}

func adSetDetail(id, name string) *advertising.ObjectDetail {
	return &advertising.ObjectDetail{
		Object: &advertising.Object{
			MetaID: id, AdAccountID: adAccountUUID, Level: advertising.LevelAdSet, Name: name, DailyBudget: 3000,
			OptimizationGoal: string(advertising.GoalConversations), DestinationType: string(advertising.DestinationWhatsApp),
		},
		Budget: &advertising.Budget{Kind: advertising.BudgetDaily, Amount: 3000},
		Bid:    advertising.Bid{Strategy: advertising.BidLowestCost},
		Targeting: &advertising.Targeting{
			Locations: []advertising.GeoLocation{{Kind: advertising.LocationCountry, Key: "BR", Name: "Brasil"}}, AgeMin: 18, AgeMax: 65,
			Interests: []advertising.TargetRef{{ID: "6003", Name: "Marketing digital"}},
		},
		Placements: &advertising.Placements{Automatic: true},
	}
}

func adDetail(id, text string) *advertising.ObjectDetail {
	return &advertising.ObjectDetail{
		Object:   &advertising.Object{MetaID: id, AdAccountID: adAccountUUID, Level: advertising.LevelAd, Name: "Anúncio " + id, DestinationType: string(advertising.DestinationWhatsApp)},
		Creative: &advertising.CreativeDraft{Format: advertising.FormatImage, PrimaryText: text, Media: advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "m-" + id}},
	}
}

type manageManager struct {
	stubAdManager
	objects map[string]*advertising.Object
}

func (m *manageManager) CheckStatus(_ context.Context, _, id string, _ bool) (*advertising.Object, error) {
	object, ok := m.objects[id]
	if !ok {
		return nil, advertising.ErrObjectNotFound
	}
	return object, nil
}

type bulkBackend struct {
	*manageEditor
	status *manageManager
}

func (b bulkBackend) CheckStatus(ctx context.Context, ws, id string, on bool) (*advertising.Object, error) {
	return b.status.CheckStatus(ctx, ws, id, on)
}

func (bulkBackend) SetStatus(context.Context, string, string, bool) (*advertising.Object, error) {
	return nil, errors.New("not used")
}

type manageBulk struct {
	backend   bulkBackend
	statusIDs []string
	on        *bool
	change    *advertising.BulkChange
	applied   *advertising.ObjectEdit
	appliedTo []string
	failing   map[string]error
}

func (b *manageBulk) results(ids []string) []adsuc.BulkResult {
	out := make([]adsuc.BulkResult, 0, len(ids))
	for _, id := range ids {
		if err := b.failing[id]; err != nil {
			out = append(out, adsuc.BulkResult{MetaID: id, Err: err})
			continue
		}
		out = append(out, adsuc.BulkResult{MetaID: id, Object: &advertising.Object{MetaID: id, Name: "Item " + id}})
	}
	return out
}

func (b *manageBulk) SetStatus(_ context.Context, _ string, ids []string, on bool) ([]adsuc.BulkResult, error) {
	b.statusIDs, b.on = ids, &on
	return b.results(ids), nil
}

func (b *manageBulk) CheckStatus(ctx context.Context, ws string, ids []string, on bool) ([]adsuc.BulkResult, error) {
	return adsuc.NewBulkUseCase(b.backend).CheckStatus(ctx, ws, ids, on)
}

func (b *manageBulk) CheckEdit(ctx context.Context, ws string, ids []string, change advertising.BulkChange) ([]adsuc.BulkResult, error) {
	return adsuc.NewBulkUseCase(b.backend).CheckEdit(ctx, ws, ids, change)
}

func (b *manageBulk) CheckApply(ctx context.Context, ws string, ids []string, edit advertising.ObjectEdit) ([]adsuc.BulkResult, error) {
	return adsuc.NewBulkUseCase(b.backend).CheckApply(ctx, ws, ids, edit)
}

func (b *manageBulk) Edit(_ context.Context, _ string, ids []string, change advertising.BulkChange) ([]adsuc.BulkResult, error) {
	b.statusIDs, b.change = ids, &change
	return b.results(ids), nil
}

func (b *manageBulk) Apply(_ context.Context, _ string, ids []string, edit advertising.ObjectEdit) ([]adsuc.BulkResult, error) {
	b.appliedTo, b.applied = ids, &edit
	return b.results(ids), nil
}

type manageSpendCap struct {
	account *advertising.AdAccount
	calls   int
	cap     *int64
}

func (s *manageSpendCap) CheckSpendCap(_ context.Context, _, _ string, cap *int64) (*advertising.AdAccount, error) {
	if err := s.account.Allows(advertising.UseBilling); err != nil {
		return nil, err
	}
	if cap != nil {
		if err := advertising.ValidateSpendCap(*cap, s.account.AmountSpent); err != nil {
			return nil, err
		}
	}
	return s.account, nil
}

func (s *manageSpendCap) SetSpendCap(_ context.Context, _, _ string, cap *int64) (*advertising.AdAccount, error) {
	s.calls++
	s.cap = cap
	return adminAdAccount(), nil
}

type manageRuns struct {
	run      *adsuc.ReportRun
	ran      []adsuc.ReportRunInput
	exported *adsuc.ReportExportInput
}

func (r *manageRuns) Run(_ context.Context, in adsuc.ReportRunInput) (*adsuc.ReportRun, error) {
	r.ran = append(r.ran, in)
	return r.run, nil
}

func (r *manageRuns) Export(_ context.Context, in adsuc.ReportExportInput) (*advertising.ReportExport, error) {
	r.exported = &in
	return &advertising.ReportExport{ID: "e-1", Name: in.Name, Range: in.Range, Rows: r.run.Rows()}, nil
}

type manageReports struct{ reports []*advertising.SavedReport }

func (r manageReports) List(context.Context, string) ([]*advertising.SavedReport, error) {
	return r.reports, nil
}

type manageFixture struct {
	tools    []copilot.Tool
	account  *advertising.AdAccount
	editor   *manageEditor
	bulk     *manageBulk
	spendCap *manageSpendCap
	runs     *manageRuns
	reports  *manageReports
}

func newManageFixture() *manageFixture {
	f := &manageFixture{
		account: adminAdAccount(),
		editor: &manageEditor{edited: map[string]advertising.ObjectEdit{}, details: map[string]*advertising.ObjectDetail{
			"120201": adSetDetail("120201", "Conjunto SP"),
			"120202": adSetDetail("120202", "Conjunto RJ"),
			"120301": adDetail("120301", "Fale com a gente hoje"),
			"120302": adDetail("120302", "Promoção de hoje"),
			"120200": {Object: &advertising.Object{MetaID: "120200", AdAccountID: adAccountUUID, Level: advertising.LevelCampaign, Name: "Leads outubro"}},
		}},
		bulk:     &manageBulk{failing: map[string]error{}},
		spendCap: &manageSpendCap{},
		runs:     &manageRuns{},
		reports:  &manageReports{},
	}
	manager := &manageManager{objects: map[string]*advertising.Object{
		"120200": {MetaID: "120200", Name: "Leads outubro", Level: advertising.LevelCampaign},
		"120201": {MetaID: "120201", Name: "Conjunto SP", Level: advertising.LevelAdSet},
	}}
	f.bulk.backend, f.spendCap.account = bulkBackend{manageEditor: f.editor, status: manager}, f.account
	ads := AdsDeps{Accounts: manageAccounts{account: f.account}, Assets: stubAdAssets{}, Manage: manager, Editor: f.editor}
	f.tools = AdManageTools(AdManageDeps{
		Bulk: f.bulk, SpendCap: f.spendCap, Runs: f.runs, Reports: f.reports, Now: func() time.Time { return adTestClock },
	}, ads)
	return f
}

func (f *manageFixture) tool(t *testing.T, name string) copilot.Tool {
	t.Helper()
	for _, tool := range f.tools {
		if tool.Definition().Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %s", name)
	return nil
}

func fieldMap(fields []copilot.Field) map[string]string {
	out := map[string]string{}
	for _, f := range fields {
		out[f.Key] = f.Value
	}
	return out
}

func validateTool(tool copilot.Tool, args map[string]interface{}) error {
	return tool.(copilot.Validator).Validate(context.Background(), adContext, args)
}

func describeTool(tool copilot.Tool, args map[string]interface{}) map[string]string {
	return fieldMap(tool.(copilot.Describer).Describe(context.Background(), adContext, args))
}

func TestEveryManageToolIsGatedCheckedAndDescribed(t *testing.T) {
	for _, tool := range newManageFixture().tools {
		def, meta := tool.Definition(), tool.Meta()
		if meta.Resource == "" || meta.Action == "" {
			t.Errorf("%s has no permission", def.Name)
		}
		for name := range def.Parameters {
			if strings.Contains(name, "workspace") {
				t.Errorf("%s lets the model pick the workspace", def.Name)
			}
		}
		if !meta.Mutating {
			continue
		}
		if _, ok := tool.(copilot.Validator); !ok {
			t.Errorf("%s changes things without Validate", def.Name)
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Errorf("%s changes things without Describe", def.Name)
		}
	}
}

func TestEditAdSetMergesTheAudienceAndKeepsWhatWasNotAsked(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "edit_ad_set")
	args := map[string]interface{}{"meta_id": "120201", "age_min": 25, "interests": []interface{}{}}
	if err := validateTool(tool, args); err != nil {
		t.Fatal(err)
	}
	if len(f.editor.checked) == 0 || f.editor.checked[0] != "120201" {
		t.Fatalf("the use case check must run before approval, checked %v", f.editor.checked)
	}
	fields := describeTool(tool, args)
	if fields["age"] != "18 a 65+ anos → 25 a 65+ anos" || fields["interests"] != "Marketing digital → nenhum (a Meta encontra o público)" {
		t.Fatalf("fields %+v", fields)
	}
	if _, shown := fields["locations"]; shown {
		t.Fatalf("unchanged locations must not be shown as a change: %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	edit := f.editor.edited["120201"]
	if edit.Targeting == nil || edit.Targeting.AgeMin != 25 || edit.Targeting.AgeMax != 65 || len(edit.Targeting.Interests) != 0 ||
		len(edit.Targeting.Locations) != 1 || edit.Targeting.Locations[0].Key != "BR" || edit.Budget != nil {
		t.Fatalf("edit %+v", edit)
	}
}

func TestEditAdSetKeepsTheBudgetKindAndEndsAtMidnightInTheAccountTimezone(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "edit_ad_set")
	args := map[string]interface{}{"meta_id": "120201", "budget": 50.0, "end_date": "2026-10-31"}
	fields := describeTool(tool, args)
	if fields["budget"] != "BRL 30,00 por dia → BRL 50,00 por dia" || fields["ends"] != "sem data de término → até 31/10/2026" {
		t.Fatalf("fields %+v", fields)
	}
	tool.Execute(context.Background(), adContext, args)
	edit := f.editor.edited["120201"]
	want := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	if edit.Budget == nil || *edit.Budget != (advertising.Budget{Kind: advertising.BudgetDaily, Amount: 5000}) || edit.EndAt == nil || !edit.EndAt.Equal(want) {
		t.Fatalf("edit %+v", edit)
	}
}

func TestEditAdSetChangesTheBidStrategyWithItsAmount(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "edit_ad_set")
	args := map[string]interface{}{"meta_id": "120201", "bid_strategy": "COST_CAP", "bid_amount": 12.5}
	if fields := describeTool(tool, args); fields["bid"] != "menor custo, sem limite → limite de custo de BRL 12,50" {
		t.Fatalf("fields %+v", fields)
	}
	tool.Execute(context.Background(), adContext, args)
	if bid := f.editor.edited["120201"].Bid; bid == nil || *bid != (advertising.Bid{Strategy: advertising.BidCostCap, Amount: 1250}) {
		t.Fatalf("bid %+v", bid)
	}
}

func TestEditAdSetSwitchesToChosenPlatforms(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "edit_ad_set")
	args := map[string]interface{}{"meta_id": "120201", "placements": []interface{}{"instagram", "facebook"}}
	if fields := describeTool(tool, args); fields["placements"] != "automáticos (Advantage+) → Instagram, Facebook" {
		t.Fatalf("fields %+v", fields)
	}
	tool.Execute(context.Background(), adContext, args)
	if p := f.editor.edited["120201"].Placements; p == nil || p.Automatic || len(p.Platforms) != 2 {
		t.Fatalf("placements %+v", p)
	}
}

func TestEditAdSetRefusesWhatTheDomainRefusesBeforeApproval(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"invented id":             {"meta_id": "conjunto sp", "age_min": 25},
		"nothing to change":       {"meta_id": "120201"},
		"audience on a campaign":  {"meta_id": "120200", "age_min": 25},
		"placements on campaign":  {"meta_id": "120200", "placements": []interface{}{"automatic"}},
		"age below the minimum":   {"meta_id": "120201", "age_min": 10},
		"automatic and platforms": {"meta_id": "120201", "placements": []interface{}{"automatic", "instagram"}},
		"budget kind of an ad":    {"meta_id": "120301", "budget": 20.0},
	}
	for name, args := range cases {
		f := newManageFixture()
		if err := validateTool(f.tool(t, "edit_ad_set"), args); !errors.Is(err, errInvalidArgs) {
			t.Errorf("%s: err %v", name, err)
		}
		if len(f.editor.edited) != 0 {
			t.Errorf("%s: edited before approval", name)
		}
	}
}

func TestSpendCapShowsTheCurrentAndNewLimitAndSendsMinorUnits(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "set_ad_spend_cap")
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "amount": 8000.0}
	if err := validateTool(tool, args); err != nil {
		t.Fatal(err)
	}
	fields := describeTool(tool, args)
	if fields["from"] != "BRL 5000,00 no total" || fields["to"] != "BRL 8000,00 no total" || fields["spent"] != "BRL 1200,00" || fields["account"] != "Loja" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK || f.spendCap.cap == nil || *f.spendCap.cap != 800_000 {
		t.Fatalf("result %+v cap %v", result, f.spendCap.cap)
	}
}

func TestSpendCapCanBeRemoved(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "set_ad_spend_cap")
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "remove": true}
	if fields := describeTool(tool, args); fields["to"] != "sem limite" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK || f.spendCap.calls != 1 || f.spendCap.cap != nil {
		t.Fatalf("result %+v cap %v", result, f.spendCap.cap)
	}
}

func TestSpendCapFollowsTheDomainRules(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "set_ad_spend_cap")
	if err := validateTool(tool, map[string]interface{}{"ad_account_id": adAccountUUID, "amount": 1000.0}); !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "BRL 1200,00") {
		t.Fatalf("a cap below what was spent must be refused: %v", err)
	}
	if err := validateTool(tool, map[string]interface{}{"ad_account_id": adAccountUUID, "amount": 9000.0, "remove": true}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("amount and remove together must be refused: %v", err)
	}
	f.account.Tasks = []string{advertising.TaskAdvertise}
	if err := validateTool(tool, map[string]interface{}{"ad_account_id": adAccountUUID, "amount": 9000.0}); !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "administrador") {
		t.Fatalf("only an admin changes billing: %v", err)
	}
	if f.spendCap.calls != 0 {
		t.Fatal("nothing must reach Meta before approval")
	}
}

func TestTheTextCardShowsWhatTheAdSaysTodayNextToTheChange(t *testing.T) {
	f := newManageFixture()
	tool := NewEditAdTextTool(AdsDeps{Accounts: manageAccounts{account: f.account}, Editor: f.editor, Bulk: f.bulk})
	args := map[string]interface{}{"meta_id": "120301", "field": "primaryText", "value": "Fale com a gente agora"}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); err != nil {
		t.Fatal(err)
	}
	fields := fieldMap(tool.(copilot.Describer).Describe(context.Background(), adContext, args))
	if fields["from"] != `"Fale com a gente hoje"` || fields["change"] != `passa a ser "Fale com a gente agora"` {
		t.Fatalf("fields %+v", fields)
	}
}
