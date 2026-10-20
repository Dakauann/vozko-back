package formtools

import (
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/shared"
)

func TestFormToolsShipEveryToolOnce(t *testing.T) {
	want := []string{
		"create_form_automation", "create_whatsapp_form", "export_whatsapp_form_responses", "list_whatsapp_forms",
		"publish_whatsapp_form", "send_whatsapp_form", "whatsapp_form_responses", "whatsapp_form_results",
	}
	got := map[string]bool{}
	for _, tool := range FormTools(FormDeps{}) {
		got[tool.Definition().Name] = true
	}
	if len(got) != len(want) {
		t.Fatalf("tools = %v", got)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("%s is missing", name)
		}
	}
}

func TestEveryFormChangeIsCheckedAndDescribedBeforeApproval(t *testing.T) {
	for _, tool := range FormTools(FormDeps{}) {
		if !tool.Meta().Mutating {
			continue
		}
		if _, ok := tool.(copilot.Validator); !ok {
			t.Errorf("%s has no preflight", tool.Definition().Name)
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Errorf("%s has no approval card", tool.Definition().Name)
		}
	}
}

func TestEveryFormToolResolvesItsCapabilities(t *testing.T) {
	for _, tool := range FormTools(FormDeps{}) {
		keys := requirementKeys(tool)
		if len(keys) == 0 {
			t.Errorf("%s declares no capability", tool.Definition().Name)
		}
		m := tool.Meta()
		if !requires(tool, string(m.Resource)+":"+string(m.Action)) {
			t.Errorf("%s gates on %s:%s outside its capability %v", tool.Definition().Name, m.Resource, m.Action, keys)
		}
	}
}

func TestFormToolDescriptionsCarryNoLongDash(t *testing.T) {
	for _, tool := range FormTools(FormDeps{}) {
		def := tool.Definition()
		text := def.Description
		for name, p := range def.Parameters {
			text += "\n" + name + " " + p.Description
		}
		if shared.HasLongDash(text) {
			t.Errorf("%s carries a long dash", def.Name)
		}
		if strings.TrimSpace(def.Description) == "" {
			t.Errorf("%s has no description", def.Name)
		}
	}
}

func TestAnUnknownCapabilityFailsClosed(t *testing.T) {
	entries := capabilityEntries("whatsapp_forms.nao_existe")
	if len(entries) != 1 || entries[0].Resource == "whatsapp_forms" {
		t.Fatalf("entries = %+v", entries)
	}
}
