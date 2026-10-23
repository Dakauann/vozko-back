package studiotools

import (
	"sort"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools/adtools"
	"vozko/usecases/copilot/copilottools/skilltools"
	"vozko/usecases/copilot/copilottools/tooltest"
	"vozko/usecases/copilot/skills"
)

func schemaChars(def tools.Definition) int {
	return tooltest.SchemaChars(def)
}

func studioViewTools(t *testing.T, kind copilot.StudioKind) []tools.Definition {
	t.Helper()
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	view := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: kind}
	candidates := append(StudioTools(StudioDeps{}), adtools.AllAdTools(adtools.AdsDeps{}, adtools.AdManageDeps{}, adtools.AdGrowthDeps{})...)
	candidates = append(candidates, skilltools.NewLoadSkillTool(library))
	var defs []tools.Definition
	for _, tool := range candidates {
		if view.Offers(tool) {
			defs = append(defs, copilot.DefinitionOn(tool, view))
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

var studioSchemaBudgets = map[string]int{
	"studio_edit_video":     10500,
	"studio_edit_image":     7200,
	"studio_op_guide":       600,
	"load_skill":            17500,
	"studio_look":           3000,
	"studio_read":           900,
	"studio_export":         2600,
	"studio_library_search": 1900,
}

const studioOtherToolBudget = 7500

var studioViewBudgets = map[copilot.StudioKind]int{copilot.StudioImage: 52000, copilot.StudioVideo: 62000}

const studioFieldDescriptionBudget = 1200

func TestTheStudioToolSchemasStayWithinTheirBudgets(t *testing.T) {
	for _, kind := range []copilot.StudioKind{copilot.StudioImage, copilot.StudioVideo} {
		total := 0
		for _, def := range studioViewTools(t, kind) {
			size := schemaChars(def)
			total += size
			budget, ok := studioSchemaBudgets[def.Name]
			if !ok {
				budget = studioOtherToolBudget
			}
			if size > budget {
				t.Errorf("%s grew to %d characters, over its budget of %d; move craft into a skill or trim repeated wording before raising the budget", def.Name, size, budget)
			}
		}
		if total > studioViewBudgets[kind] {
			t.Errorf("the %s studio sends %d characters of tool schema on every call, over the budget of %d", kind, total, studioViewBudgets[kind])
		}
	}
}

func TestNoStudioFieldDescriptionTurnsIntoAGuide(t *testing.T) {
	var walk func(owner, name string, p tools.Parameter)
	walk = func(owner, name string, p tools.Parameter) {
		if len(p.Description) > studioFieldDescriptionBudget && name != "font_id" {
			t.Errorf("%s.%s has a %d character description; field descriptions carry what the field takes, guides belong in the tool description or a skill", owner, name, len(p.Description))
		}
		if p.Items != nil {
			for child, cp := range p.Items.Properties {
				walk(owner, child, cp)
			}
		}
	}
	for _, kind := range []copilot.StudioKind{copilot.StudioImage, copilot.StudioVideo} {
		for _, def := range append(studioViewTools(t, kind), studioEditFor(kind).Definition()) {
			for name, p := range def.Parameters {
				walk(def.Name, name, p)
			}
		}
	}
}
