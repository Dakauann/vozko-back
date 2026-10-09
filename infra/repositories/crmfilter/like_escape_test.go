package crmfilter

import (
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
)

func TestContainsMatchesWildcardsLiterally(t *testing.T) {
	typed := `50%_off\now`
	want := `%50\%\_off\\now%`
	tests := []struct {
		name    string
		compile func() (string, []interface{}, error)
		args    []interface{}
	}{
		{"column", func() (string, []interface{}, error) {
			return compileColumn(FieldMapping{Expr: "o.title", Kind: crmfilter.KindText}, pred(crmfilter.FieldQuery, crmfilter.OpContains, typed))
		}, []interface{}{want}},
		{"text template", func() (string, []interface{}, error) {
			return compileText(FieldMapping{Style: StyleText, Template: "(a ILIKE ? OR b ILIKE ?)", Params: 2}, pred(crmfilter.FieldQuery, crmfilter.OpContains, typed))
		}, []interface{}{want, want}},
		{"custom text field", func() (string, []interface{}, error) {
			return compileJSONBCustom(boundCustom(crmfilter.KindString, "nota", crmfilter.OpContains, typed), cfCol)
		}, []interface{}{"nota", want}},
		{"lead search", func() (string, []interface{}, error) {
			return LeadDescriptor{Alias: "leads", WorkspaceID: "ws-1"}.compileQuery(pred(crmfilter.FieldQuery, crmfilter.OpContains, typed))
		}, []interface{}{"ws-1", want, "ws-1", want, "ws-1", want, want, "ws-1", want}},
		{"lead number", func() (string, []interface{}, error) {
			return LeadDescriptor{Alias: "leads", WorkspaceID: "ws-1"}.compileNumber(pred(crmfilter.FieldNumber, crmfilter.OpContains, typed))
		}, []interface{}{want, "ws-1", want}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, args, err := tc.compile()
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if !reflect.DeepEqual(args, tc.args) {
				t.Fatalf("args = %#v, want %#v", args, tc.args)
			}
		})
	}
}
