package tools_usecase

import (
	"context"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

type aliasRegistry struct{ defs []tools.Definition }

func (r aliasRegistry) Definitions() []tools.Definition                          { return r.defs }
func (r aliasRegistry) DefinitionsFor(v tools.ToolVisibility) []tools.Definition { return r.defs }
func (r aliasRegistry) Execute(context.Context, string, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (r aliasRegistry) ExecuteWithConfig(context.Context, string, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (r aliasRegistry) Handler(string) (tools.Handler, bool) { return nil, false }

func TestRetiredToolNamesResolveToTheCurrentTool(t *testing.T) {
	for _, tc := range []struct{ legacy, want string }{
		{"send_whatsapp_media", ToolNameSendMedia},
		{"send_whatsapp_image", ToolNameSendMedia},
		{"send_whatsapp_button_message", ToolNameSendOptions},
	} {
		if got := CanonicalToolName(tc.legacy); got != tc.want {
			t.Errorf("CanonicalToolName(%q) = %q, want %q", tc.legacy, got, tc.want)
		}
	}
}

func TestCurrentNamesAreUnchangedAndCaseInsensitive(t *testing.T) {
	if got := CanonicalToolName(ToolNameSendMedia); got != ToolNameSendMedia {
		t.Errorf("a current name was rewritten: %q", got)
	}
	if got := CanonicalToolName("  Send_WhatsApp_Media "); got != ToolNameSendMedia {
		t.Errorf("alias lookup is not normalised: %q", got)
	}
	if got := CanonicalToolName("Some_Other_Tool"); got != "some_other_tool" {
		t.Errorf("unknown name = %q", got)
	}
}

func TestAnAgentBoundToTheOldNameStillGetsTheTool(t *testing.T) {
	reg := aliasRegistry{defs: []tools.Definition{{
		Name:       ToolNameSendMedia,
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
	}}}

	resolved := ResolveTools(reg, []agent.ToolBinding{{Name: "send_whatsapp_media"}},
		agent.ToolVisibilityMessaging, ToolResolverOptions{})

	if len(resolved.Definitions) != 1 {
		t.Fatalf("definitions = %+v, the saved binding was silently dropped", resolved.Definitions)
	}
	if resolved.Definitions[0].Name != ToolNameSendMedia {
		t.Errorf("resolved %q, want the current tool", resolved.Definitions[0].Name)
	}
}

func TestOldAndNewBindingsDoNotDuplicateTheTool(t *testing.T) {
	reg := aliasRegistry{defs: []tools.Definition{{
		Name:       ToolNameSendMedia,
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
	}}}

	resolved := ResolveTools(reg, []agent.ToolBinding{
		{Name: "send_whatsapp_media"},
		{Name: ToolNameSendMedia},
	}, agent.ToolVisibilityMessaging, ToolResolverOptions{})

	if len(resolved.Definitions) != 1 {
		t.Errorf("definitions = %+v, want the tool exactly once", resolved.Definitions)
	}
}

func TestToolNamesDoNotNameAChannel(t *testing.T) {
	for _, name := range []string{ToolNameSendMedia, ToolNameSendOptions} {
		if got := CanonicalToolName(name); got != name {
			t.Fatalf("%q is itself an alias", name)
		}
		for _, brand := range []string{"whatsapp", "telegram", "instagram"} {
			if contains(name, brand) {
				t.Errorf("tool name %q names a channel", name)
			}
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}

func TestADirectNameMatchWinsOverTheAlias(t *testing.T) {
	reg := aliasRegistry{defs: []tools.Definition{{
		Name:       "send_whatsapp_media",
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
	}}}

	resolved := ResolveTools(reg, []agent.ToolBinding{{Name: "send_whatsapp_media"}},
		agent.ToolVisibilityMessaging, ToolResolverOptions{})

	if len(resolved.Definitions) != 1 {
		t.Fatalf("definitions = %+v, the alias hid a tool that was registered under the old name",
			resolved.Definitions)
	}
	if resolved.Definitions[0].Name != "send_whatsapp_media" {
		t.Errorf("resolved %q, want the registry's own name", resolved.Definitions[0].Name)
	}
}
