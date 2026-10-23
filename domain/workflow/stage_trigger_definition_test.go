package workflow

import "testing"

func TestTheStageTriggerIsInTheCatalogSoTheEditorOffersIt(t *testing.T) {
	def, found := NodeCatalogMap(BuiltinDefinitions())[NodeTypeTriggerStageAdded]
	if !found {
		t.Fatal("trigger_stage_added has no definition, so the palette hides it")
	}
	if def.Category != NodeCategoryTrigger || def.TestBehavior != NodeTestTrigger || !DefinitionAllowedForType(def, WorkflowTypeMessages) || def.Label == "" || def.Icon == "" {
		t.Fatalf("definition = %+v", def)
	}
	if len(def.ConfigSchema) != 1 {
		t.Fatalf("schema = %+v, want only the stage choice", def.ConfigSchema)
	}
	stage := def.ConfigSchema[0]
	if stage.Key != DataKeyStageID || stage.OptionsSource != "stages" || stage.Required {
		t.Fatalf("stage field = %+v, want an optional stage picker (empty means any stage)", stage)
	}
	if empty, _ := def.DefaultConfig[DataKeyStageID].(string); empty != "" {
		t.Fatalf("default config = %v", def.DefaultConfig)
	}
}
