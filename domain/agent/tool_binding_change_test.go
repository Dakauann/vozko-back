package agent

import "testing"

func TestBindingChangedFindsANewOrReconfiguredBinding(t *testing.T) {
	const name = "record_consent"
	granted := ToolBinding{Name: name, Visibility: []ToolVisibility{ToolVisibilityMessaging}, Config: map[string]interface{}{"purpose": "ofertas"}}
	other := ToolBinding{Name: "record_opt_out", Visibility: []ToolVisibility{ToolVisibilityMessaging}}
	cases := []struct {
		name     string
		stored   []ToolBinding
		proposed []ToolBinding
		want     bool
	}{
		{"added", []ToolBinding{other}, []ToolBinding{other, granted}, true},
		{"kept as it was", []ToolBinding{granted}, []ToolBinding{other, granted}, false},
		{"purpose changed", []ToolBinding{granted}, []ToolBinding{{Name: name, Visibility: granted.Visibility, Config: map[string]interface{}{"purpose": "cobranca"}}}, true},
		{"purpose removed", []ToolBinding{granted}, []ToolBinding{{Name: name, Visibility: granted.Visibility}}, true},
		{"visibility changed", []ToolBinding{granted}, []ToolBinding{{Name: name, Config: granted.Config}}, true},
		{"removed", []ToolBinding{granted}, []ToolBinding{other}, false},
		{"never bound", nil, []ToolBinding{other}, false},
		{"nothing stored", nil, []ToolBinding{granted}, true},
		{"empty and absent config are the same", []ToolBinding{{Name: name}}, []ToolBinding{{Name: name, Config: map[string]interface{}{}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BindingChanged(tc.stored, tc.proposed, name); got != tc.want {
				t.Fatalf("BindingChanged = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBindingsAddedListsEveryNewBindingOfATool(t *testing.T) {
	const name = "add_sheet_row"
	sheetA := ToolBinding{Name: name, Config: map[string]interface{}{"sheet_id": "a"}}
	sheetB := ToolBinding{Name: name, Config: map[string]interface{}{"sheet_id": "b"}}
	other := ToolBinding{Name: "record_opt_out"}
	cases := []struct {
		name     string
		stored   []ToolBinding
		proposed []ToolBinding
		want     int
	}{
		{"a second sheet next to a kept one", []ToolBinding{sheetA}, []ToolBinding{sheetA, sheetB}, 1},
		{"kept as they were", []ToolBinding{sheetA, sheetB}, []ToolBinding{sheetB, sheetA, other}, 0},
		{"first bindings", nil, []ToolBinding{sheetA, sheetB}, 2},
		{"removed", []ToolBinding{sheetA}, []ToolBinding{other}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BindingsAdded(tc.stored, tc.proposed, name); len(got) != tc.want {
				t.Fatalf("BindingsAdded = %v, want %d", got, tc.want)
			}
		})
	}
}
