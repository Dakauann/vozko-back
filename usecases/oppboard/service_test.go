package oppboard_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/opportunity"
	"vozko/domain/savedview"
	"vozko/domain/stage"
)

type fakeSearcher struct {
	searchInputs []opportunity.SearchByFilterInput
	sumInputs    []opportunity.SearchByFilterInput

	countByCol map[string]int64
	sumByCol   map[string]int64
}

func colKey(f crmfilter.Filter) string {
	if len(f.Groups) == 0 {
		return "__all__"
	}
	last := f.Groups[len(f.Groups)-1]
	if len(last.Predicates) == 0 {
		return "__all__"
	}
	p := last.Predicates[0]
	if p.Operator == crmfilter.OpIsEmpty {
		return "__empty__"
	}
	if len(p.Values) > 0 {
		return p.Values[0]
	}
	return "__all__"
}

func (f *fakeSearcher) SearchByFilter(in opportunity.SearchByFilterInput) ([]*opportunity.Opportunity, int64, error) {
	f.searchInputs = append(f.searchInputs, in)
	key := colKey(in.Filter)
	n := f.countByCol[key]
	rows := make([]*opportunity.Opportunity, 0, n)
	for i := int64(0); i < n; i++ {
		rows = append(rows, &opportunity.Opportunity{ID: key, StageID: key})
	}
	return rows, n, nil
}

func (f *fakeSearcher) SumValueByFilter(in opportunity.SearchByFilterInput) (int64, error) {
	f.sumInputs = append(f.sumInputs, in)
	return f.sumByCol[colKey(in.Filter)], nil
}

type fakeStages struct{ stages []*stage.Stage }

func (f *fakeStages) EnsureDefaultOpportunityPipeline(string) (string, error) {
	return "default-opp-pipeline", nil
}
func (f *fakeStages) ListByPipeline(_, pipelineID string) ([]*stage.Stage, error) {
	var out []*stage.Stage
	for _, s := range f.stages {
		if s.PipelineID == pipelineID {
			out = append(out, s)
		}
	}
	return out, nil
}

type denyingAuth struct{ allowed bool }

func (a denyingAuth) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, a.allowed
}

type scopingAuth struct {
	scope   conversation.DepartmentAccessScope
	allowed bool
}

func (a scopingAuth) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return a.scope, a.allowed
}

func lastPredicate(t *testing.T, f crmfilter.Filter) crmfilter.Predicate {
	t.Helper()
	if len(f.Groups) == 0 || len(f.Groups[len(f.Groups)-1].Predicates) == 0 {
		t.Fatalf("filter has no appended predicate: %#v", f)
	}
	return f.Groups[len(f.Groups)-1].Predicates[0]
}

func TestGetBoard_StageAxis(t *testing.T) {
	searcher := &fakeSearcher{
		countByCol: map[string]int64{"s1": 3, "s2": 1},
		sumByCol:   map[string]int64{"s1": 490000, "s2": 120000},
	}
	stages := &fakeStages{stages: []*stage.Stage{
		{ID: "s1", Name: "Novo", Color: "#3b82f6", PipelineID: "pipe1"},
		{ID: "s2", Name: "Ganho", Color: "#10b981", PipelineID: "pipe1"},
		{ID: "s3", Name: "OutroPipeline", PipelineID: "pipeX"},
	}}
	svc := NewService(searcher, stages, nil)

	board, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1",
		PipelineID:  "pipe1",
		GroupBy:     savedview.GroupByStage,
	})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if board.GroupBy != "stage" {
		t.Fatalf("groupBy = %q, want stage", board.GroupBy)
	}
	if len(board.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(board.Columns))
	}

	c0 := board.Columns[0]
	if c0.ID != "s1" || c0.Name != "Novo" || c0.Color != "#3b82f6" {
		t.Fatalf("column0 identity mismatch: %#v", c0)
	}
	if c0.Total != 3 || c0.ValueTotal != 490000 {
		t.Fatalf("column0 totals: Total=%d ValueTotal=%d, want 3 / 490000", c0.Total, c0.ValueTotal)
	}
	if len(c0.Entries) != 3 {
		t.Fatalf("column0 entries = %d, want 3", len(c0.Entries))
	}

	c1 := board.Columns[1]
	if c1.Total != 1 || c1.ValueTotal != 120000 {
		t.Fatalf("column1 totals: Total=%d ValueTotal=%d, want 1 / 120000", c1.Total, c1.ValueTotal)
	}

	p := lastPredicate(t, searcher.searchInputs[0].Filter)
	if p.Field != crmfilter.FieldStage || p.Operator != crmfilter.OpIn || len(p.Values) != 1 || p.Values[0] != "s1" {
		t.Fatalf("stage column predicate mismatch: %#v", p)
	}
	if colKey(searcher.sumInputs[0].Filter) != "s1" {
		t.Fatalf("sum input filter not aligned to column s1: %#v", searcher.sumInputs[0].Filter)
	}
}

func TestGetBoard_OwnerAxis(t *testing.T) {
	searcher := &fakeSearcher{
		countByCol: map[string]int64{"u1": 2, "__empty__": 5},
		sumByCol:   map[string]int64{"u1": 999, "__empty__": 111},
	}
	svc := NewService(searcher, &fakeStages{}, nil)

	base := crmfilter.Filter{Groups: []crmfilter.Group{
		{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldStatus, Operator: crmfilter.OpEquals, Values: []string{"open"}}}},
	}}

	board, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1",
		GroupBy:     savedview.GroupByOwner,
		Owners:      []Owner{{ID: "u1", Name: "Ana"}},
		Filter:      base,
	})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Columns) != 2 {
		t.Fatalf("columns = %d, want 2 (owner + unassigned)", len(board.Columns))
	}
	if board.Columns[0].ID != "u1" || board.Columns[0].Total != 2 || board.Columns[0].ValueTotal != 999 {
		t.Fatalf("owner column mismatch: %#v", board.Columns[0])
	}
	last := board.Columns[1]
	if last.ID != "__unassigned__" || last.Total != 5 || last.ValueTotal != 111 {
		t.Fatalf("unassigned column mismatch: %#v", last)
	}

	f := searcher.searchInputs[0].Filter
	if len(f.Groups) != 2 {
		t.Fatalf("owner column filter should be base+1 groups, got %d", len(f.Groups))
	}
	if f.Groups[0].Predicates[0].Field != crmfilter.FieldStatus {
		t.Fatalf("base predicate not preserved: %#v", f.Groups[0])
	}
	p := lastPredicate(t, f)
	if p.Field != crmfilter.FieldOwner || p.Operator != crmfilter.OpIn || p.Values[0] != "u1" {
		t.Fatalf("owner predicate mismatch: %#v", p)
	}
	pe := lastPredicate(t, searcher.searchInputs[1].Filter)
	if pe.Field != crmfilter.FieldOwner || pe.Operator != crmfilter.OpIsEmpty {
		t.Fatalf("unassigned predicate mismatch: %#v", pe)
	}
}

func TestGetBoard_CustomAxis(t *testing.T) {
	searcher := &fakeSearcher{
		countByCol: map[string]int64{"enterprise": 4},
		sumByCol:   map[string]int64{"enterprise": 700000},
	}
	svc := NewService(searcher, &fakeStages{}, nil)

	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByCustom}); err != ErrGroupByKeyMissing {
		t.Fatalf("expected ErrGroupByKeyMissing, got %v", err)
	}

	board, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1",
		GroupBy:     savedview.GroupByCustom,
		GroupByKey:  "segmento",
		Options:     []Option{{Value: "enterprise", Name: "Enterprise"}},
	})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Columns) != 1 {
		t.Fatalf("columns = %d, want 1", len(board.Columns))
	}
	col := board.Columns[0]
	if col.ID != "enterprise" || col.Name != "Enterprise" || col.Total != 4 || col.ValueTotal != 700000 {
		t.Fatalf("custom column mismatch: %#v", col)
	}
	p := lastPredicate(t, searcher.searchInputs[0].Filter)
	if p.Field != crmfilter.FieldCustom || p.Key != "segmento" || p.Operator != crmfilter.OpEquals || p.Values[0] != "enterprise" {
		t.Fatalf("custom predicate mismatch: %#v", p)
	}
}

func TestGetBoard_UnsupportedGroupBy(t *testing.T) {
	svc := NewService(&fakeSearcher{}, &fakeStages{}, nil)
	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByLabel}); err != ErrUnsupportedGroupBy {
		t.Fatalf("expected ErrUnsupportedGroupBy, got %v", err)
	}
}

func TestGetBoard_AuthorizerDenies(t *testing.T) {
	svc := NewService(&fakeSearcher{}, &fakeStages{}, denyingAuth{allowed: false})
	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByStage}); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGetBoard_ThreadsDepartmentScope(t *testing.T) {
	searcher := &fakeSearcher{countByCol: map[string]int64{"s1": 1}, sumByCol: map[string]int64{"s1": 100}}
	stages := &fakeStages{stages: []*stage.Stage{{ID: "s1", Name: "New", PipelineID: "p1"}}}
	auth := scopingAuth{
		scope:   conversation.DepartmentAccessScope{DepartmentIDs: []string{"dept-1"}, Restrict: true},
		allowed: true,
	}
	svc := NewService(searcher, stages, auth)

	if _, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1", UserID: "u1", IsAdmin: false,
		GroupBy: savedview.GroupByStage, PipelineID: "p1",
	}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(searcher.searchInputs) == 0 {
		t.Fatal("no searches issued")
	}
	for i, in := range searcher.searchInputs {
		if !in.RestrictDepartments || len(in.DepartmentIDs) != 1 || in.DepartmentIDs[0] != "dept-1" {
			t.Errorf("search %d scope not threaded: restrict=%v depts=%v", i, in.RestrictDepartments, in.DepartmentIDs)
		}
		if in.AssigneeOverrideUserID != "u1" {
			t.Errorf("search %d owner override = %q, want u1", i, in.AssigneeOverrideUserID)
		}
	}
	for i, in := range searcher.sumInputs {
		if !in.RestrictDepartments {
			t.Errorf("value sum %d not scoped, column totals would leak across departments", i)
		}
	}
}

func TestGetBoard_AdminNotRestricted(t *testing.T) {
	searcher := &fakeSearcher{countByCol: map[string]int64{"s1": 1}}
	stages := &fakeStages{stages: []*stage.Stage{{ID: "s1", PipelineID: "p1"}}}
	auth := scopingAuth{scope: conversation.DepartmentAccessScope{Restrict: false}, allowed: true}
	svc := NewService(searcher, stages, auth)

	if _, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1", UserID: "admin", IsAdmin: true,
		GroupBy: savedview.GroupByStage, PipelineID: "p1",
	}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for i, in := range searcher.searchInputs {
		if in.RestrictDepartments {
			t.Errorf("search %d restricted for admin, should be workspace-wide", i)
		}
	}
}

func TestGetList_PassesFilterThrough(t *testing.T) {
	searcher := &fakeSearcher{countByCol: map[string]int64{"__all__": 7}}
	svc := NewService(searcher, &fakeStages{}, nil)

	_, total, err := svc.GetList(ListInput{WorkspaceID: "ws1", SortField: "value", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if total != 7 {
		t.Fatalf("total = %d, want 7", total)
	}
	in := searcher.searchInputs[0]
	if len(in.Filter.Groups) != 0 {
		t.Fatalf("list should pass the base filter with no appended predicate, got %#v", in.Filter)
	}
	if in.SortField != "value" || in.SortOrder != "desc" {
		t.Fatalf("sort not passed through: %+v", in)
	}
}
