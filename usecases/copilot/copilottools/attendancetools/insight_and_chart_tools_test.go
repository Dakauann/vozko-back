package attendancetools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/audience"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type fakeAudienceStats struct {
	calls int
	got   audience.ListInput
}

func (f *fakeAudienceStats) Execute(_ context.Context, in audience.ListInput) (*audience.Stats, error) {
	f.calls++
	f.got = in
	return &audience.Stats{
		Counters: audience.Counters{ConversationAnalyzed: 4, DispositionSale: 1},
		Subjects: []audience.SubjectCount{{Key: "plano", Label: "Plano", Count: 3}},
	}, nil
}

type fakeAudienceList struct{ got audience.ListInput }

func (f *fakeAudienceList) Execute(_ context.Context, in audience.ListInput) (*shared.PaginatedResult[*audience.Analysis], error) {
	f.got = in
	items := make([]*audience.Analysis, 0, 10)
	for i := 0; i < 10; i++ {
		items = append(items, &audience.Analysis{SubjectID: "c", Summary: "resumo", Transcript: "User: dado sensível"})
	}
	return &shared.PaginatedResult[*audience.Analysis]{Items: items}, nil
}

func TestConversationInsightsReadsTheLatestRevisionOnly(t *testing.T) {
	stats, list := &fakeAudienceStats{}, &fakeAudienceList{}
	cc := tooltest.OwnerOn(screen)
	res := NewConversationInsightsTool(stats, list, fixedNow).Execute(context.Background(), cc, map[string]interface{}{"disposition": "sale", "examples": 9.0})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	if !stats.got.LatestOnly || stats.got.Disposition != audience.DispositionSale || stats.got.From.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("stats input = %+v", stats.got)
	}
	if list.got.Options.Pagination.PageSize != audience.MaxDigestExamples {
		t.Fatalf("page size = %d, want the example cap", list.got.Options.Pagination.PageSize)
	}
	data := res.Data.(map[string]interface{})
	if got := data["examples"].([]audience.ConversationExample); len(got) != audience.MaxDigestExamples {
		t.Fatalf("examples = %d", len(got))
	}
	if _, err := cc.Datasets.Get(data["subjects_dataset"].(copilot.DatasetPreview).DatasetID); err != nil {
		t.Fatalf("subjects dataset not stored: %v", err)
	}
}

func TestConversationInsightsIsForWorkspaceWideReadersOnly(t *testing.T) {
	stats := &fakeAudienceStats{}
	res := NewConversationInsightsTool(stats, &fakeAudienceList{}, fixedNow).Execute(context.Background(), tooltest.MemberOf("d1"), nil)
	if res.Status != copilot.StatusDenied || stats.calls != 0 {
		t.Fatalf("status = %v calls = %d", res.Status, stats.calls)
	}
}

func TestConversationInsightsRefusesAnUnknownFilter(t *testing.T) {
	stats := &fakeAudienceStats{}
	res := NewConversationInsightsTool(stats, &fakeAudienceList{}, fixedNow).Execute(context.Background(), tooltest.OwnerOn(screen), map[string]interface{}{"qualification": "vip"})
	if res.Status != copilot.StatusError || stats.calls != 0 {
		t.Fatalf("status = %v calls = %d, want a silent widening refused", res.Status, stats.calls)
	}
}

func TestCalculate(t *testing.T) {
	res := NewCalculateTool().Execute(context.Background(), copilot.Context{}, map[string]interface{}{"expressions": []interface{}{"pct_change(80, 100)"}})
	steps, _ := res.Data.(map[string]interface{})["steps"].([]calculatedStep)
	if res.Status != copilot.StatusOK || len(steps) != 1 || steps[0].Result != 25.0 {
		t.Fatalf("res = %+v", res)
	}
	res = NewCalculateTool().Execute(context.Background(), copilot.Context{}, map[string]interface{}{"expressions": []interface{}{"a = 2", "1/0"}})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "step 2") {
		t.Fatalf("res = %+v, want division by zero refused and the step named", res)
	}
}

func TestCalculateNamesStepsForLayoutGeometry(t *testing.T) {
	res := NewCalculateTool().Execute(context.Background(), copilot.Context{}, map[string]interface{}{"expressions": []interface{}{
		"w = 640",
		"x = round(w / 2 + 100 * cos(rad(60)), 2)",
		"x / w",
	}})
	steps, _ := res.Data.(map[string]interface{})["steps"].([]calculatedStep)
	if res.Status != copilot.StatusOK || len(steps) != 3 || steps[1] != (calculatedStep{Name: "x", Expression: "round(w / 2 + 100 * cos(rad(60)), 2)", Result: 370}) {
		t.Fatalf("res = %+v", res)
	}
}

func TestCalculateIsOfferedOnEveryScreen(t *testing.T) {
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioImage}
	for _, view := range []copilot.View{{}, studio} {
		if !view.Offers(NewCalculateTool()) {
			t.Fatalf("calculate must be offered on %+v", view)
		}
	}
}

func storedDataset(t *testing.T, cc copilot.Context) string {
	t.Helper()
	d := copilot.NewDataset("Equipe", []copilot.Column{
		{Key: "name", Kind: copilot.ColumnText},
		{Key: "resolved", Kind: copilot.ColumnNumber},
	})
	for i := 0; i < 40; i++ {
		_ = d.AddRow(string(rune('a'+i)), float64(i))
	}
	return cc.Datasets.Put(d).ID
}

func TestQueryDatasetPages(t *testing.T) {
	cc := tooltest.OwnerOn(screen)
	id := storedDataset(t, cc)
	res := NewQueryDatasetTool().Execute(context.Background(), cc, map[string]interface{}{"dataset_id": id, "sort_by": "resolved", "descending": true, "limit": 2.0})
	page := res.Data.(copilot.DatasetPreview)
	if res.Status != copilot.StatusOK || len(page.Rows) != 2 || page.Rows[0][1] != 39.0 {
		t.Fatalf("res = %+v", res)
	}
	if res := NewQueryDatasetTool().Execute(context.Background(), cc, map[string]interface{}{"dataset_id": "ds99"}); res.Status != copilot.StatusError {
		t.Fatalf("unknown dataset = %+v", res)
	}
}

func TestRenderChartFromADatasetHandle(t *testing.T) {
	cc := tooltest.OwnerOn(screen)
	id := storedDataset(t, cc)
	res := NewRenderChartTool().Execute(context.Background(), cc, map[string]interface{}{
		"type": "horizontal_bar", "title": "Top 5", "dataset_id": id, "x": "name", "y": []interface{}{"resolved"},
		"sort_by": "resolved", "descending": true, "limit": 5.0,
	})
	if res.Status != copilot.StatusOK || res.Chart == nil || len(res.Chart.Categories) != 5 {
		t.Fatalf("res = %+v", res)
	}
	if data := res.Data.(map[string]interface{}); data["points"] != 5 {
		t.Fatalf("data = %+v", data)
	}
}

func TestRenderChartFromInlineNumbers(t *testing.T) {
	res := NewRenderChartTool().Execute(context.Background(), tooltest.OwnerOn(screen), map[string]interface{}{
		"type":  "bar",
		"title": "Agosto x Setembro",
		"data": map[string]interface{}{
			"categories": []interface{}{"Agosto", "Setembro"},
			"series":     []interface{}{map[string]interface{}{"label": "Resolvidas", "values": []interface{}{120.0, 150.0}}},
		},
	})
	if res.Status != copilot.StatusOK || res.Chart == nil || res.Chart.Series[0].Label != "Resolvidas" {
		t.Fatalf("res = %+v", res)
	}
	res = NewRenderChartTool().Execute(context.Background(), tooltest.OwnerOn(screen), map[string]interface{}{
		"type": "bar",
		"data": map[string]interface{}{
			"categories": []interface{}{"a"},
			"series":     []interface{}{map[string]interface{}{"label": "x", "values": []interface{}{"doze"}}},
		},
	})
	if res.Status != copilot.StatusError || res.Chart != nil {
		t.Fatalf("res = %+v, want text values refused", res)
	}
}
