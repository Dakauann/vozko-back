package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/workspace"
	copilotuc "vozko/usecases/copilot"
)

func TestTheEvalOffersTheOperationGuideBesideTheCompactEditTool(t *testing.T) {
	registry, err := evalRegistry(studio.KindImage)
	if err != nil {
		t.Fatal(err)
	}
	cc := copilot.Context{
		WorkspaceID: evalWorkspace,
		UserID:      "eval-user",
		Role:        workspace.RoleOwner,
		View:        copilot.View{Surface: copilot.SurfaceStudio, ProjectID: evalProject, ProjectKind: copilot.StudioImage},
		Mode:        copilot.ModeEdit,
	}
	offered := map[string]string{}
	for _, def := range copilotuc.NewDriver(cc, "eval", registry, allowAll{}, allowAll{}, uuid.NewString).Tools() {
		offered[def.Name] = def.Description
	}
	if _, ok := offered["studio_op_guide"]; !ok {
		t.Fatal("the eval run cannot fetch the operation fields")
	}
	if edit, ok := offered["studio_edit_image"]; !ok || !strings.Contains(edit, "studio_op_guide") {
		t.Fatalf("the eval run does not get the compact image edit tool: %q", edit)
	}
}
