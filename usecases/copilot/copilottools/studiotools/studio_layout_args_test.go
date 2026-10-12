package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioImageEditCarriesLayouts(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_layout", "ref": "cta", "layer_ids": []interface{}{"pill", "label"}, "layout_direction": "horizontal", "layout_gap": 16.0, "layout_padding": []interface{}{20.0, 40.0}, "layout_align_cross": "center", "layout_width": "hug", "layout_height": "hug"},
		map[string]interface{}{"op": "set_layout_item", "layer_id": "label", "item_width": "hug", "item_height": "hug"},
		map[string]interface{}{"op": "set_layout", "group_id": "@cta", "layout_auto_gap": false},
		map[string]interface{}{"op": "set_layout_item", "layer_id": "badge", "item_absolute": true},
		map[string]interface{}{"op": "remove_layout", "group_id": "@cta"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_layout"`, `"layout_direction":"horizontal"`, `"layout_gap":16`, `"layout_padding":[20,40]`, `"layout_align_cross":"center"`, `"item_width":"hug"`, `"layout_auto_gap":false`, `"item_absolute":true`, `"op":"remove_layout"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	props := edit.Definition().Parameters["operations"].Items.Properties
	if strings.Join(props["item_width"].Enum, ",") != "fixed,fill,hug" || props["layout_padding"].Items == nil || props["layout_padding"].Items.Type != "number" {
		t.Fatalf("layout schema = %+v / %+v", props["item_width"], props["layout_padding"])
	}
	if !strings.Contains(studioImageEditGuide, "set_layout") || !strings.Contains(studioImageEditGuide, "set_layout_item") {
		t.Fatal("the image guide does not explain auto layout")
	}
}
