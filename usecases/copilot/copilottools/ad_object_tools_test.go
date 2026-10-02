package copilottools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	adsuc "vozko/usecases/advertising"
)

func (m *stubAdManager) CheckCopy(_ context.Context, _, id string, req advertising.CopyRequest) (*advertising.Object, error) {
	if id != "120200" || (req.ParentID != "" && req.ParentID != "120300") {
		return nil, advertising.FieldError("parentId", "not_available")
	}
	return &advertising.Object{MetaID: id, Name: "Leads outubro", Level: advertising.LevelCampaign}, nil
}

func (m *stubAdManager) Copy(ctx context.Context, ws, id string, req advertising.CopyRequest) (string, error) {
	if _, err := m.CheckCopy(ctx, ws, id, req); err != nil {
		return "", err
	}
	m.copied = append(m.copied, req)
	return "120999", nil
}

func (m *stubAdManager) CheckLifecycle(_ context.Context, _, id string, _ advertising.Lifecycle) (*advertising.Object, error) {
	if id != "120200" {
		return nil, advertising.ErrObjectNotFound
	}
	return &advertising.Object{MetaID: id, Name: "Leads outubro", Level: advertising.LevelAdSet}, nil
}

func (m *stubAdManager) Lifecycle(ctx context.Context, ws, id string, action advertising.Lifecycle) (*advertising.Object, error) {
	object, err := m.CheckLifecycle(ctx, ws, id, action)
	if err != nil {
		return nil, err
	}
	m.lifecycles = append(m.lifecycles, action)
	object.Status = advertising.StatusArchived
	return object, nil
}

type stubAdLive struct{ query advertising.LiveQuery }

func (s *stubAdLive) Insights(_ context.Context, q advertising.LiveQuery) (*adsuc.LiveReport, error) {
	q.Level = q.Level.OrCampaign()
	if q.Range.Since.IsZero() {
		q.Range, _ = advertising.NewDateRange("2026-09-01", "2026-09-30")
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	s.query = q
	return &adsuc.LiveReport{
		Account: &advertising.AdAccount{Name: "Loja", Currency: "BRL"},
		Query:   q,
		Rows: []advertising.LiveRow{
			{ObjectID: "c-1", Dimensions: map[advertising.Breakdown]string{advertising.BreakdownAge: "25-34"}, Values: advertising.LiveMetrics{Metrics: advertising.Metrics{SpendMicros: 1_000_000}, Reach: 100}},
			{ObjectID: "c-1", Dimensions: map[advertising.Breakdown]string{advertising.BreakdownAge: "35-44"}, Values: advertising.LiveMetrics{Metrics: advertising.Metrics{SpendMicros: 9_000_000}, Reach: 400}},
		},
	}, nil
}

func fieldsOf(fields []copilot.Field) map[string]string {
	out := map[string]string{}
	for _, f := range fields {
		out[f.Key] = f.Value
	}
	return out
}

func TestDuplicateChecksTheTargetBeforeTheCardAndCopiesOnApproval(t *testing.T) {
	deps, manager, _ := adDeps()
	tool := NewDuplicateAdTool(deps)
	bad := map[string]interface{}{"meta_id": "120200", "parent_id": "120301"}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, bad); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	args := map[string]interface{}{"meta_id": "120200", "deep_copy": true, "name_suffix": " - cópia"}
	if got := fieldsOf(tool.(copilot.Describer).Describe(context.Background(), adContext, args)); got["level"] != "campanha" || got["copies"] != "o item e tudo dentro dele" {
		t.Fatalf("fields %v", got)
	}
	result := tool.Execute(context.Background(), adContext, args)
	if result.Status != copilot.StatusOK || len(manager.copied) != 1 || !manager.copied[0].DeepCopy || manager.copied[0].NameSuffix != " - cópia" {
		t.Fatalf("result %+v copied %+v", result, manager.copied)
	}
}

func TestArchiveAndDeleteAreSeparateToolsWithTheirOwnPermission(t *testing.T) {
	deps, manager, _ := adDeps()
	archive, del := NewArchiveAdTool(deps), NewDeleteAdTool(deps)
	if archive.Meta().Action == del.Meta().Action {
		t.Fatal("delete must need its own permission")
	}
	if got := fieldsOf(del.(copilot.Describer).Describe(context.Background(), adContext, map[string]interface{}{"meta_id": "120200"})); got["change"] != "excluir de vez, sem como desfazer" {
		t.Fatalf("fields %v", got)
	}
	if err := archive.(copilot.Validator).Validate(context.Background(), adContext, map[string]interface{}{"meta_id": "999"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	if result := archive.Execute(context.Background(), adContext, map[string]interface{}{"meta_id": "120200"}); result.Status != copilot.StatusOK ||
		len(manager.lifecycles) != 1 || manager.lifecycles[0] != advertising.LifecycleArchive {
		t.Fatalf("result %+v lifecycles %v", result, manager.lifecycles)
	}
}

func TestBreakdownSortsSegmentsBySpendAndPassesTheWindow(t *testing.T) {
	deps, _, _ := adDeps()
	live := &stubAdLive{}
	deps.Live = live
	result := NewAdsBreakdownTool(deps).Execute(context.Background(), adContext, map[string]interface{}{
		"ad_account_id": adAccountUUID, "breakdowns": []interface{}{"age"}, "window": "7d_click",
	})
	if result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	rows := result.Data.(map[string]interface{})["rows"].([]map[string]interface{})
	if rows[0]["reach"] != int64(400) || len(live.query.Windows) != 1 || live.query.Windows[0] != advertising.Window7DayClick {
		t.Fatalf("rows %v query %+v", rows, live.query)
	}
}

func TestBreakdownRefusesACombinationMetaDoesNotAccept(t *testing.T) {
	deps, _, _ := adDeps()
	deps.Live = &stubAdLive{}
	result := NewAdsBreakdownTool(deps).Execute(context.Background(), adContext, map[string]interface{}{
		"ad_account_id": adAccountUUID, "breakdowns": []interface{}{"age", "publisher_platform"},
	})
	if result.Status != copilot.StatusError {
		t.Fatalf("result %+v", result)
	}
}
