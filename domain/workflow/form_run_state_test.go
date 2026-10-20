package workflow

import "testing"

func TestFormAnswersInTheRunStateReadByTheirDottedName(t *testing.T) {
	state := NewRunState()
	state.Set("form.email", "ana@example.com")
	state.Set(DataKeyFormResponseID, "resp-1")
	state.Set("contact", map[string]interface{}{"email": "nested@example.com"})

	cases := []struct {
		text string
		want string
	}{
		{"{{form.email}}", "ana@example.com"},
		{"{{var.form.email}}", "ana@example.com"},
		{"{{form_response_id}}", "resp-1"},
		{"{{contact.email}}", "nested@example.com"},
		{"{{form.unknown}}", "{{form.unknown}}"},
	}
	for _, tc := range cases {
		if got := Interpolate(tc.text, &state, nil); got != tc.want {
			t.Errorf("Interpolate(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
	if got, ok := ResolveVariable("form.email", &state); !ok || got != "ana@example.com" {
		t.Errorf("ResolveVariable(form.email) = %v, %v", got, ok)
	}
}

func TestTheCatalogOffersTheFormSubmittedTrigger(t *testing.T) {
	def, ok := NodeCatalogMap(BuiltinDefinitions())[NodeTypeTriggerFormSubmitted]
	if !ok {
		t.Fatal("trigger_form_submitted is missing from the catalog")
	}
	if def.Category != NodeCategoryTrigger || !DefinitionAllowedForType(def, WorkflowTypeMessages) || DefinitionAllowedForType(def, WorkflowTypeVoice) {
		t.Fatalf("definition = %+v", def)
	}
	var field *ConfigField
	for i := range def.ConfigSchema {
		if def.ConfigSchema[i].Key == FormTriggerFormIDsKey {
			field = &def.ConfigSchema[i]
		}
	}
	if field == nil || field.Type != "multi-select" || field.Required || field.OptionsSource == "" {
		t.Fatalf("form_ids field = %+v", field)
	}
	keys := map[string]bool{}
	for _, k := range def.OutputKeys {
		keys[k.Key] = true
	}
	if !keys[DataKeyFormResponseID] || !keys[DataKeyFormID] {
		t.Fatalf("output keys = %+v", def.OutputKeys)
	}
}
