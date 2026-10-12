package copilottools_test

import (
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/tools"
)

func schemaViews() []snapshotView {
	return append([]snapshotView{{name: "studio-without-project", view: copilot.View{Surface: copilot.SurfaceStudio}}}, snapshotViews...)
}

func listsWithoutItems(path string, p tools.Parameter) []string {
	if p.Items == nil {
		if p.Type == "array" {
			return []string{path}
		}
		return nil
	}
	return itemsWithoutItems(path, *p.Items)
}

func itemsWithoutItems(path string, items tools.ParameterItems) []string {
	var missing []string
	if items.Type == "array" {
		if items.Items == nil {
			return []string{path + "[]"}
		}
		missing = append(missing, itemsWithoutItems(path+"[]", *items.Items)...)
	}
	for name, child := range items.Properties {
		missing = append(missing, listsWithoutItems(path+"."+name, child)...)
	}
	return missing
}

func everyDefinition(t *testing.T) map[string]tools.Definition {
	t.Helper()
	defs := map[string]tools.Definition{}
	for _, tool := range registeredTools(t) {
		def := tool.Definition()
		defs["reference "+def.Name] = def
		for _, entry := range schemaViews() {
			if entry.view.Offers(tool) {
				defs[entry.name+" "+def.Name] = copilot.DefinitionOn(tool, entry.view)
			}
		}
	}
	return defs
}

func TestEveryListInEveryToolDeclaresItsItemsAtAnyDepth(t *testing.T) {
	for key, def := range everyDefinition(t) {
		for name, p := range def.Parameters {
			for _, path := range listsWithoutItems(name, p) {
				t.Errorf("%s: %s is a list without items", key, path)
			}
		}
	}
}

func TestEveryToolSchemaIsValidJSONSchemaAsProvidersRequire(t *testing.T) {
	defs := everyDefinition(t)
	if len(defs) < len(registeredTools(t)) {
		t.Fatalf("only %d definitions walked", len(defs))
	}
	for key, def := range defs {
		for _, issue := range tools.SchemaIssues(def.JSONSchema()) {
			t.Errorf("%s: %s", key, issue)
		}
	}
}

func TestTheCompactStudioEditSchemasAreWalked(t *testing.T) {
	defs := everyDefinition(t)
	for _, key := range []string{"studio-image studio_edit_image", "studio-video studio_edit_video", "reference studio_edit_image", "reference studio_edit_video"} {
		if _, ok := defs[key]; !ok {
			t.Fatalf("%s was not walked", key)
		}
	}
	if len(defs["studio-image studio_edit_image"].Description) >= len(defs["reference studio_edit_image"].Description) {
		t.Fatal("the studio view must be the compact edit schema")
	}
}
