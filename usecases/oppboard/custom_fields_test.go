package oppboard_usecase

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/savedview"
)

type fakeFields struct {
	defs  []*customfield.Definition
	err   error
	calls int
}

func (f *fakeFields) ListByObject(_ string, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	f.calls++
	if objectType != customfield.ObjectOpportunity {
		return nil, errors.New("wrong object")
	}
	return f.defs, f.err
}

func opportunityFields() *fakeFields {
	return &fakeFields{defs: []*customfield.Definition{
		{Key: "segmento", Type: customfield.TypeSelect, Options: []string{"enterprise", "smb"}},
		{Key: "score", Type: customfield.TypeNumber},
		{Key: "classificacao", Type: customfield.TypeSelect, Options: []string{"Positivo"}, Sensitive: true, LegalBasis: "consent"},
	}}
}

func customFilterOf(key string, op crmfilter.Operator, values ...string) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: key, Operator: op, Values: values},
	}}}}
}

func TestTheBoardBindsItsCustomColumnsToTheirDefinitions(t *testing.T) {
	searcher := &fakeSearcher{}
	fields := opportunityFields()
	svc := NewService(searcher, &fakeStages{}, denyingAuth{allowed: true}, fields)

	if _, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1",
		GroupBy:     savedview.GroupByCustom,
		GroupByKey:  "segmento",
		Options:     []Option{{Value: "enterprise"}, {Value: "smb"}},
	}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for _, in := range searcher.searchInputs {
		if kind, ok := lastPredicate(t, in.Filter).BoundKind(); !ok || kind != crmfilter.KindEnum {
			t.Fatalf("column predicate bound = %v, %v", kind, ok)
		}
	}
	if fields.calls != 1 {
		t.Fatalf("definitions read %d times, want once per board", fields.calls)
	}
}

func TestTheListBindsCustomPredicates(t *testing.T) {
	searcher := &fakeSearcher{}
	svc := NewService(searcher, &fakeStages{}, denyingAuth{allowed: true}, opportunityFields())

	if _, _, err := svc.GetList(ListInput{WorkspaceID: "ws1", Filter: customFilterOf("score", crmfilter.OpGreaterEq, "10")}); err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if kind, ok := searcher.searchInputs[0].Filter.Groups[0].Predicates[0].BoundKind(); !ok || kind != crmfilter.KindNumber {
		t.Fatalf("bound = %v, %v", kind, ok)
	}
}

func TestCustomFilterRefusals(t *testing.T) {
	boom := errors.New("definitions down")
	cases := []struct {
		name   string
		fields DefinitionLister
		filter crmfilter.Filter
		want   error
	}{
		{"unknown key", opportunityFields(), customFilterOf("nope", crmfilter.OpEquals, "x"), customfield.ErrFilterUnknownKey},
		{"operator unsuited to the type", opportunityFields(), customFilterOf("segmento", crmfilter.OpGreaterEq, "1"), customfield.ErrFilterOperator},
		{"sensitive key without permission", opportunityFields(), customFilterOf("classificacao", crmfilter.OpEquals, "Positivo"), customfield.ErrFilterSensitive},
		{"definitions unreadable", &fakeFields{err: boom}, customFilterOf("segmento", crmfilter.OpEquals, "smb"), boom},
		{"no definition source", nil, customFilterOf("segmento", crmfilter.OpEquals, "smb"), customfield.ErrDefinitionsUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			searcher := &fakeSearcher{}
			svc := NewService(searcher, &fakeStages{}, denyingAuth{allowed: true}, tc.fields)
			if _, _, err := svc.GetList(ListInput{WorkspaceID: "ws1", Filter: tc.filter}); !errors.Is(err, tc.want) {
				t.Fatalf("GetList() error = %v, want %v", err, tc.want)
			}
			if len(searcher.searchInputs) != 0 {
				t.Fatal("the searcher ran on a refused filter")
			}
		})
	}
}

func TestASensitiveCustomColumnNeedsThePermission(t *testing.T) {
	svc := NewService(&fakeSearcher{}, &fakeStages{}, denyingAuth{allowed: true}, opportunityFields())
	in := BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByCustom, GroupByKey: "classificacao", Options: []Option{{Value: "Positivo"}}}
	if _, err := svc.GetBoard(in); !errors.Is(err, customfield.ErrFilterSensitive) {
		t.Fatalf("GetBoard() error = %v, want ErrFilterSensitive", err)
	}
	in.Viewer = customfield.Viewer{ReadsSensitive: true}
	if _, err := svc.GetBoard(in); err != nil {
		t.Fatalf("GetBoard() with the permission error = %v", err)
	}
}

func TestFiltersWithoutCustomPredicatesNeverReadDefinitions(t *testing.T) {
	fields := &fakeFields{err: errors.New("must not be read")}
	svc := NewService(&fakeSearcher{}, &fakeStages{}, denyingAuth{allowed: true}, fields)
	stage := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{"s1"}},
	}}}}
	if _, _, err := svc.GetList(ListInput{WorkspaceID: "ws1", Filter: stage}); err != nil {
		t.Fatalf("GetList() error = %v", err)
	}
	if fields.calls != 0 {
		t.Fatalf("definitions read %d times for a filter without custom fields", fields.calls)
	}
}
