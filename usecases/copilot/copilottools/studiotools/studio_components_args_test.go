package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioImageEditCarriesComponents(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "create_component", "ref": "cta", "layer_ids": []interface{}{"pill", "label"}, "name": "Botão"},
		map[string]interface{}{"op": "add_instance", "component_id": "@cta", "artboard_id": "story", "w": 0.6},
		map[string]interface{}{"op": "set_overrides", "layer_id": "@cta", "part_id": "label", "text": "Compre", "font_weight": 700.0, "italic": false, "hidden": false},
		map[string]interface{}{"op": "set_overrides", "layer_id": "@cta", "part_id": "photo", "media_id": "m-1", "crop": map[string]interface{}{"x": 0.0, "y": 0.0, "w": 0.5, "h": 1.0}},
		map[string]interface{}{"op": "reset_overrides", "layer_id": "@cta", "part_id": "label", "reset": []interface{}{"font_weight", "media_id"}},
		map[string]interface{}{"op": "swap_component", "layer_id": "@cta", "component_id": "badge"},
		map[string]interface{}{"op": "detach_instance", "layer_id": "@cta"},
		map[string]interface{}{"op": "update_artboard", "artboard_id": "badge", "component": false},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"create_component"`, `"component_id":"@cta"`, `"part_id":"label"`, `"italic":false`, `"hidden":false`, `"reset":["font_weight","media_id"]`, `"op":"swap_component"`, `"op":"detach_instance"`, `"component":false`, `"crop":{`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	reset := edit.Definition().Parameters["operations"].Items.Properties["reset"]
	if reset.Items == nil || strings.Join(reset.Items.Enum, ",") != "text,fill,stroke,font_id,font_weight,italic,media_id,crop,icon_id,hidden" {
		t.Fatalf("reset schema = %+v", reset)
	}
	if !strings.Contains(studioImageEditGuide, "set_overrides") || !strings.Contains(studioImageEditGuide, "create_component") {
		t.Fatal("the image guide does not explain components")
	}
}
