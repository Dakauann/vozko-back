package emaildesigntools

import (
	"sort"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools/attendancetools"
	"vozko/usecases/copilot/copilottools/platformtools"
	"vozko/usecases/copilot/copilottools/skilltools"
	"vozko/usecases/copilot/copilottools/tooltest"
	"vozko/usecases/copilot/skills"
)

func emailViewTools(t *testing.T) []tools.Definition {
	t.Helper()
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	candidates := append(EmailDesignTools(Deps{}), skilltools.NewLoadSkillTool(library), platformtools.NewAskUserTool(), attendancetools.NewCalculateTool())
	var defs []tools.Definition
	for _, tool := range candidates {
		if designView.Offers(tool) {
			defs = append(defs, copilot.DefinitionOn(tool, designView))
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

var emailSchemaBudgets = map[string]int{
	"email_edit":           4200,
	"email_generate_image": 2100,
	"email_look":           1400,
	"email_read":           700,
}

const (
	emailOtherToolBudget = 9000
	emailViewBudget      = 20000
	emailFieldBudget     = 400
)

func TestTheEmailEditorToolSchemasStayWithinTheirBudgets(t *testing.T) {
	total := 0
	for _, def := range emailViewTools(t) {
		size := tooltest.SchemaChars(def)
		total += size
		budget, ok := emailSchemaBudgets[def.Name]
		if !ok {
			budget = emailOtherToolBudget
		}
		if size > budget {
			t.Errorf("%s grew to %d characters, over its budget of %d; move craft into the design-de-email skill before raising the budget", def.Name, size, budget)
		}
	}
	if total > emailViewBudget {
		t.Errorf("the email editor sends %d characters of tool schema on every call, over the budget of %d", total, emailViewBudget)
	}
}

func TestNoEmailFieldDescriptionTurnsIntoAGuide(t *testing.T) {
	var walk func(owner, name string, p tools.Parameter)
	walk = func(owner, name string, p tools.Parameter) {
		if len(p.Description) > emailFieldBudget {
			t.Errorf("%s.%s has a %d character description; craft belongs in the skill", owner, name, len(p.Description))
		}
		if p.Items != nil {
			for child, cp := range p.Items.Properties {
				walk(owner, child, cp)
			}
		}
	}
	for _, def := range emailViewTools(t) {
		for name, p := range def.Parameters {
			walk(def.Name, name, p)
		}
	}
}
