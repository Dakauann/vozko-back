package copilottools

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

func idList(ids ...string) []interface{} {
	out := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
	}
	return out
}

func TestBulkTurnOffChecksEveryItemBeforeApproval(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "bulk_turn_off_ads")
	args := map[string]interface{}{"meta_ids": idList("120200", "120201")}
	if err := validateTool(tool, args); err != nil {
		t.Fatal(err)
	}
	fields := describeTool(tool, args)
	if fields["change"] != "desligar" || fields["items"] != "2 itens: Leads outubro (campanha), Conjunto SP (conjunto)" {
		t.Fatalf("fields %+v", fields)
	}
	unknown := map[string]interface{}{"meta_ids": idList("120200", "999")}
	if err := validateTool(tool, unknown); !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "999") {
		t.Fatalf("an unknown item must block the whole change and be named: %v", err)
	}
	result := tool.Execute(context.Background(), adContext, args)
	if result.Status != copilot.StatusOK || f.bulk.on == nil || *f.bulk.on || len(f.bulk.statusIDs) != 2 {
		t.Fatalf("result %+v bulk %+v", result, f.bulk)
	}
	if data := result.Data.(map[string]interface{}); data["changed"] != 2 || data["failed"] != 0 {
		t.Fatalf("data %+v", data)
	}
}

func TestBulkToolsRefuseInventedRepeatedOrTooManyIds(t *testing.T) {
	many := make([]string, 0, advertising.MaxBulkObjects+1)
	for i := range advertising.MaxBulkObjects + 1 {
		many = append(many, strconv.Itoa(130000+i))
	}
	cases := map[string][]interface{}{
		"invented": idList("campanha leads"),
		"repeated": idList("120200", "120200"),
		"too many": idList(many...),
		"none":     {},
	}
	for name, ids := range cases {
		f := newManageFixture()
		for _, toolName := range []string{"bulk_turn_on_ads", "bulk_turn_off_ads"} {
			if err := validateTool(f.tool(t, toolName), map[string]interface{}{"meta_ids": ids}); !errors.Is(err, errInvalidArgs) {
				t.Errorf("%s %s: err %v", toolName, name, err)
			}
		}
	}
}

func TestBulkResultsReportEachItem(t *testing.T) {
	f := newManageFixture()
	f.bulk.failing["120201"] = advertising.ErrObjectLocked
	result := f.tool(t, "bulk_turn_on_ads").Execute(context.Background(), adContext, map[string]interface{}{"meta_ids": idList("120200", "120201")})
	data := result.Data.(map[string]interface{})
	items := data["items"].([]map[string]interface{})
	if result.Status != copilot.StatusOK || data["changed"] != 1 || data["failed"] != 1 || items[1]["ok"] != false || items[1]["error"] != "itens arquivados ou excluídos não mudam mais" {
		t.Fatalf("result %+v", result)
	}
	f.bulk.failing["120200"] = advertising.ErrObjectLocked
	if all := f.tool(t, "bulk_turn_on_ads").Execute(context.Background(), adContext, map[string]interface{}{"meta_ids": idList("120200", "120201")}); all.Status != copilot.StatusError {
		t.Fatalf("nothing changed must read as an error: %+v", all)
	}
}

func TestBulkEditTextReplacesInsideEachAdAfterCheckingIt(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "bulk_edit_ads_text")
	args := map[string]interface{}{"meta_ids": idList("120301", "120302"), "field": "primaryText", "mode": "replace", "find": "hoje", "replace": "agora"}
	if err := validateTool(tool, args); err != nil {
		t.Fatal(err)
	}
	if len(f.editor.checked) != 2 {
		t.Fatalf("each item must pass the use case check, checked %v", f.editor.checked)
	}
	if fields := describeTool(tool, args); fields["items"] != "2 itens" || fields["field"] != "texto principal" || fields["change"] != "trocar \"hoje\" por \"agora\"" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	want := advertising.BulkChange{Field: advertising.BulkPrimaryText, Mode: advertising.BulkReplace, Find: "hoje", Replace: "agora"}
	if f.bulk.change == nil || *f.bulk.change != want {
		t.Fatalf("change %+v", f.bulk.change)
	}
}

func TestBulkEditTextRefusesAChangeThatTouchesNothingOrTheWrongLevel(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"text not found":     {"meta_ids": idList("120301"), "field": "primaryText", "mode": "replace", "find": "amanhã", "replace": "agora"},
		"text on an ad set":  {"meta_ids": idList("120201"), "field": "headline", "mode": "set", "value": "Novo"},
		"replace without it": {"meta_ids": idList("120301"), "field": "primaryText", "mode": "replace"},
		"empty name":         {"meta_ids": idList("120301"), "field": "name", "mode": "set", "value": " "},
	}
	for name, args := range cases {
		if err := validateTool(newManageFixture().tool(t, "bulk_edit_ads_text"), args); !errors.Is(err, errInvalidArgs) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

func TestBulkChangeAppliesOneEndDateAndBudgetToTheSameAccount(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "bulk_change_ads")
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "meta_ids": idList("120201", "120202"), "budget": 40.0, "end_date": "2026-10-31"}
	if err := validateTool(tool, args); err != nil {
		t.Fatal(err)
	}
	if fields := describeTool(tool, args); fields["items"] != "2 itens" || fields["budget"] != "BRL 40,00 em cada item, no tipo atual de cada um (diário ou total)" || fields["ends"] != "até 31/10/2026" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	want := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	edit := f.bulk.applied
	if edit == nil || len(f.bulk.appliedTo) != 2 || *edit.Budget != (advertising.Budget{Amount: 4000}) || !edit.EndAt.Equal(want) {
		t.Fatalf("applied %+v to %v", edit, f.bulk.appliedTo)
	}
}

func TestBulkChangeKeepsTheBudgetKindOfEachItem(t *testing.T) {
	f := newManageFixture()
	lifetime := f.editor.details["120202"]
	lifetime.Object.DailyBudget, lifetime.Object.LifetimeBudget = 0, 90000
	lifetime.Budget = &advertising.Budget{Kind: advertising.BudgetLifetime, Amount: 90000}
	end := "2026-12-31"
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "meta_ids": idList("120201", "120202"), "budget": 40.0, "end_date": end}
	if err := validateTool(f.tool(t, "bulk_change_ads"), args); err != nil {
		t.Fatalf("a daily and a lifetime item take the same amount in their own kinds: %v", err)
	}
	if len(f.editor.checked) != 2 {
		t.Fatalf("each item must pass the use case check, checked %v", f.editor.checked)
	}
	if _, offered := f.tool(t, "bulk_change_ads").Definition().Parameters["budget_kind"]; offered {
		t.Fatal("one budget kind for every item must not be offered")
	}
}

func TestBulkChangeRefusesOtherAccountsBeforeApproval(t *testing.T) {
	f := newManageFixture()
	f.editor.details["120202"].Object.AdAccountID = otherAdAccountUUID
	tool := f.tool(t, "bulk_change_ads")
	mixed := map[string]interface{}{"ad_account_id": adAccountUUID, "meta_ids": idList("120201", "120202"), "budget": 40.0}
	if err := validateTool(tool, mixed); !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "120202") {
		t.Fatalf("an item of another account must be refused: %v", err)
	}
	adOnly := map[string]interface{}{"ad_account_id": adAccountUUID, "meta_ids": idList("120301"), "budget": 40.0}
	if err := validateTool(tool, adOnly); !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "120301") {
		t.Fatalf("an ad has no budget of its own: %v", err)
	}
	empty := map[string]interface{}{"ad_account_id": adAccountUUID, "meta_ids": idList("120201")}
	if err := validateTool(tool, empty); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("a change without end date or budget must be refused: %v", err)
	}
	if f.bulk.applied != nil {
		t.Fatal("nothing must be applied before approval")
	}
}
