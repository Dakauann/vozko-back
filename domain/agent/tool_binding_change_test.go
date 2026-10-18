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
