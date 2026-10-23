package copilottools_test

import (
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/usecases/copilot/copilottools/emaildesigntools"
	"vozko/usecases/copilot/copilottools/studiotools"
)

func TestEveryChangeIsCheckedBeforeTheUserSeesIt(t *testing.T) {
	for _, tool := range registeredTools(t) {
		if !tool.Meta().Mutating {
			continue
		}
		if _, ok := tool.(copilot.Validator); !ok {
			t.Errorf("%s proposes changes without a preflight Validate", tool.Definition().Name)
		}
	}
}

func TestNoToolLetsTheModelChooseTheWorkspace(t *testing.T) {
	for _, tool := range registeredTools(t) {
		for name := range tool.Definition().Parameters {
			if strings.Contains(strings.ToLower(name), "workspace") {
				t.Errorf("%s takes %q: the workspace must come from the session, never from the model", tool.Definition().Name, name)
			}
		}
	}
}

func TestEveryToolDeclaresAPermission(t *testing.T) {
	for _, tool := range registeredTools(t) {
		if m := tool.Meta(); m.Resource == "" || m.Action == "" {
			t.Errorf("%s has no permission", tool.Definition().Name)
		}
	}
}

func TestChangesToAConversationShowWhoTheyTouch(t *testing.T) {
	for _, tool := range registeredTools(t) {
		def := tool.Definition()
		_, targetsConversation := def.Parameters["entry_id"]
		if !tool.Meta().Mutating || !targetsConversation {
			continue
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Errorf("%s changes a conversation but its approval card cannot name it", def.Name)
		}
	}
}

func TestConversationIdsAlwaysTravelWithTheirChannel(t *testing.T) {
	for _, tool := range registeredTools(t) {
		def := tool.Definition()
		if _, ok := def.Parameters["entry_id"]; !ok {
			continue
		}
		if _, ok := def.Parameters["entry_type"]; !ok {
			t.Errorf("%s takes entry_id without entry_type", def.Name)
		}
	}
}

func TestToolNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range registeredTools(t) {
		name := tool.Definition().Name
		if seen[name] {
			t.Errorf("duplicate tool %s", name)
		}
		seen[name] = true
	}
}

func TestSecretsNeverTravelThroughTheModel(t *testing.T) {
	for _, tool := range registeredTools(t) {
		def := tool.Definition()
		for name := range def.Parameters {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
				t.Errorf("%s offers the model a %q parameter; ask for it as a protected field instead", def.Name, name)
			}
		}
		asker, ok := tool.(copilot.SecretAsker)
		if !ok {
			continue
		}
		if !tool.Meta().Mutating {
			t.Errorf("%s asks for a secret without an approval card to type it in", def.Name)
		}
		for _, secret := range asker.Secrets(nil) {
			if _, offered := def.Parameters[secret.Key]; offered {
				t.Errorf("%s offers its secret %q to the model", def.Name, secret.Key)
			}
		}
	}
}

func TestEveryChangeNamesWhatItTouchesOnTheCard(t *testing.T) {
	for _, tool := range registeredTools(t) {
		if !tool.Meta().Mutating {
			continue
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Errorf("%s proposes a change whose approval card falls back to the raw arguments", tool.Definition().Name)
		}
	}
}

func TestStudioToolsLiveOnlyInsideTheStudio(t *testing.T) {
	video := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: copilot.StudioVideo}
	image := video
	image.ProjectKind = copilot.StudioImage
	for _, tool := range studiotools.StudioTools(studiotools.StudioDeps{}) {
		name := tool.Definition().Name
		if _, scoped := tool.(copilot.Scoped); !scoped {
			t.Fatalf("%s is not scoped to the studio", name)
		}
		for _, elsewhere := range []copilot.View{{}, {Surface: copilot.SurfaceAttendance}} {
			if elsewhere.Offers(tool) {
				t.Fatalf("%s leaks outside the studio", name)
			}
		}
		if !strings.HasPrefix(name, "studio_") {
			t.Fatalf("%s must be namespaced", name)
		}
		if !video.Offers(tool) && !image.Offers(tool) {
			t.Fatalf("%s is offered in no project", name)
		}
	}
	for name, view := range map[string]copilot.View{"studio_edit_video": video, "studio_edit_image": image, "studio_generate_music": video, "studio_generate_voiceover": video} {
		for _, tool := range studiotools.StudioTools(studiotools.StudioDeps{}) {
			if tool.Definition().Name == name && (!view.Offers(tool) || (view == video && image.Offers(tool)) || (view == image && video.Offers(tool))) {
				t.Fatalf("%s must be offered only on its project kind", name)
			}
		}
	}
}

func TestEmailEditorToolsLiveOnlyInsideTheEmailEditor(t *testing.T) {
	email := copilot.View{Surface: copilot.SurfaceEmailDesign, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"}
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: copilot.StudioImage}
	for _, tool := range emaildesigntools.EmailDesignTools(emaildesigntools.Deps{}) {
		name := tool.Definition().Name
		if name == "new_email_design" {
			continue
		}
		if !strings.HasPrefix(name, "email_") || !email.Offers(tool) {
			t.Fatalf("%s must be an email_ tool offered in the email editor", name)
		}
		for _, elsewhere := range []copilot.View{{}, {Surface: copilot.SurfaceAttendance}, studio} {
			if elsewhere.Offers(tool) {
				t.Fatalf("%s leaks outside the email editor", name)
			}
		}
	}
}

func TestEveryModeGradedToolCanBeProposed(t *testing.T) {
	for _, tool := range registeredTools(t) {
		if _, graded := tool.(copilot.Graded); !graded {
			continue
		}
		name := tool.Definition().Name
		if _, ok := tool.(copilot.Validator); !ok {
			t.Errorf("%s can be proposed in ask mode but has no preflight", name)
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Errorf("%s can be proposed in ask mode but its card would show raw arguments", name)
		}
	}
}
