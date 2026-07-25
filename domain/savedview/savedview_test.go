package savedview

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
)

func baseView() *SavedView {
	return &SavedView{
		WorkspaceID: "ws1",
		OwnerID:     "u1",
		ObjectType:  ObjectOpportunity,
		Name:        "Minhas abertas",
		GroupBy:     GroupByStage,
		Visibility:  VisibilityPrivate,
		SortDir:     SortDesc,
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{
			{Predicates: []crmfilter.Predicate{
				{Field: crmfilter.FieldOwner, Operator: crmfilter.OpEquals, Values: []string{"u1"}},
				{Field: crmfilter.FieldStatus, Operator: crmfilter.OpEquals, Values: []string{"open"}},
			}},
		}},
	}
}

func TestValidate_OK(t *testing.T) {
	if err := baseView().Validate(); err != nil {
		t.Fatalf("expected valid view, got %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SavedView)
		want   error
	}{
		{"no workspace", func(v *SavedView) { v.WorkspaceID = "" }, ErrWorkspaceRequired},
		{"no name", func(v *SavedView) { v.Name = "" }, ErrNameRequired},
		{"bad object", func(v *SavedView) { v.ObjectType = "widget" }, ErrInvalidObject},
		{"bad groupby", func(v *SavedView) { v.GroupBy = "planet" }, ErrInvalidGroupBy},
		{"custom groupby no key", func(v *SavedView) { v.GroupBy = GroupByCustom }, ErrGroupByKeyMissing},
		{"bad visibility", func(v *SavedView) { v.Visibility = "secret" }, ErrInvalidVisibility},
		{"bad sort dir", func(v *SavedView) { v.SortDir = "sideways" }, ErrInvalidSortDir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := baseView()
			tc.mutate(v)
			err := v.Validate()
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidate_PropagatesFilterError(t *testing.T) {
	v := baseView()
	v.Filter = crmfilter.Filter{Groups: []crmfilter.Group{
		{Predicates: []crmfilter.Predicate{{Field: "bogus", Operator: crmfilter.OpEquals, Values: []string{"x"}}}},
	}}
	if err := v.Validate(); !errors.Is(err, crmfilter.ErrUnknownField) {
		t.Fatalf("expected embedded filter validation to surface, got %v", err)
	}
}

func TestValidate_CustomGroupByWithKey(t *testing.T) {
	v := baseView()
	v.GroupBy = GroupByCustom
	v.GroupByKey = "origem"
	if err := v.Validate(); err != nil {
		t.Fatalf("custom group_by with a key should be valid, got %v", err)
	}
}

func TestNormalizeDefaults(t *testing.T) {
	v := &SavedView{WorkspaceID: "ws1", Name: "  Sem responsável  ", ObjectType: ObjectConversation}
	v.Normalize()
	if v.Name != "Sem responsável" {
		t.Fatalf("name should be trimmed, got %q", v.Name)
	}
	if v.GroupBy != GroupByStage {
		t.Fatalf("group_by should default to stage, got %q", v.GroupBy)
	}
	if v.Visibility != VisibilityPrivate {
		t.Fatalf("visibility should default to private, got %q", v.Visibility)
	}
	if v.SortDir != SortDesc {
		t.Fatalf("sort dir should default to desc, got %q", v.SortDir)
	}
	if err := v.Validate(); err != nil {
		t.Fatalf("normalized view should validate, got %v", err)
	}
}
