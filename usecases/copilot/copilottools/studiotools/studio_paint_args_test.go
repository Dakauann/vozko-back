package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioImageEditCarriesPatternsGradientMapsPaintListsAndEffects(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "update_layer", "layer_id": "card", "pattern": map[string]interface{}{"preset": "dots", "tile": "brick-row", "size": 24.0, "spacing_x": 0.0, "origin_x": 0.25}},
		map[string]interface{}{"op": "update_layer", "layer_id": "photo", "gradient_map": map[string]interface{}{"stops": []interface{}{map[string]interface{}{"at": 0.0, "color": "#000000"}, map[string]interface{}{"at": 1.0, "color": "#ffffff"}}, "intensity": 0.0, "reverse": true}},
		map[string]interface{}{
			"op": "update_layer", "layer_id": "title",
			"fills":   []interface{}{map[string]interface{}{"color": "#ffffff"}, map[string]interface{}{"pattern": map[string]interface{}{"media_id": "img"}, "opacity": 0.5, "blend_mode": "overlay"}},
			"strokes": []interface{}{map[string]interface{}{"color": "#000000", "width": 6.0, "align": "outside", "dash_array": []interface{}{2.0, 1.0}}},
			"effects": []interface{}{map[string]interface{}{"id": "fx-1", "type": "inner-glow", "source": "center", "spread": 0.0, "show_behind": false}},
			"clear":   []interface{}{"pattern"},
		},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"preset":"dots"`, `"tile":"brick-row"`, `"spacing_x":0`, `"origin_x":0.25`,
		`"stops":[{"at":0,"color":"#000000"},{"at":1,"color":"#ffffff"}]`, `"intensity":0`, `"reverse":true`,
		`"fills":[{"color":"#ffffff"},{"pattern":{"media_id":"img"},"opacity":0.5,"blend_mode":"overlay"}]`,
		`"width":6`, `"align":"outside"`, `"dash_array":[2,1]`,
		`"id":"fx-1"`, `"type":"inner-glow"`, `"source":"center"`, `"spread":0`, `"show_behind":false`, `"clear":["pattern"]`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	item := edit.Definition().Parameters["operations"].Items
	if pattern := item.Properties["pattern"]; pattern.Items == nil || len(pattern.Items.Properties["preset"].Enum) != 12 || len(pattern.Items.Properties["tile"].Enum) != 5 {
		t.Fatalf("pattern schema = %+v", pattern)
	}
	if strokes := item.Properties["strokes"]; strokes.Type != "array" || strokes.Items == nil || len(strokes.Items.Properties["align"].Enum) != 3 || strokes.Items.Properties["color"].Type != "string" {
		t.Fatalf("strokes schema = %+v", strokes)
	}
	if effects := item.Properties["effects"]; effects.Items == nil || len(effects.Items.Properties["type"].Enum) != 4 {
		t.Fatalf("effects schema = %+v", effects)
	}
	if stops := item.Properties["gradient_map"].Items.Properties["stops"]; stops.Items == nil || len(stops.Items.Required) != 1 {
		t.Fatalf("gradient map stops schema = %+v", stops)
	}
	for _, name := range []string{"pattern", "gradient_map", "fills", "strokes", "effects"} {
		if !copilottools.Contains(item.Properties["clear"].Items.Enum, name) {
			t.Fatalf("clear does not offer %s", name)
		}
	}
	video := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items
	if effects := video.Properties["effects"]; effects.Type != "array" || effects.Items == nil || len(effects.Items.Properties["type"].Enum) != 4 {
		t.Fatalf("the video editor must offer the same effect list: %+v", effects)
	}
	if video.Properties["fills"].Type != "array" || video.Properties["strokes"].Type != "array" || video.Properties["pattern"].Type != "" {
		t.Fatal("the video editor offers fills and strokes lists and no pattern")
	}
}

func TestStudioImageGuideNamesNoVendor(t *testing.T) {
	guide := strings.ToLower(studioImageEditGuide)
	for _, vendor := range []string{"figma", "adobe", "photoshop", "illustrator", "canva", "affinity", "anthropic", "remotion"} {
		if strings.Contains(guide, vendor) {
			t.Fatalf("the image guide names %s", vendor)
		}
	}
}

func TestStudioImageEditCarriesRepeats(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "repeat_layers", "ref": "flor", "layer_ids": []interface{}{"petal"}, "repeat": map[string]interface{}{"mode": "radial", "count": 10.0, "start_angle": 0.0, "upright": false}},
		map[string]interface{}{"op": "set_repeat", "group_id": "@flor", "repeat": map[string]interface{}{"mode": "grid", "rows": 2.0, "columns": 3.0, "flip_rows": "none"}},
		map[string]interface{}{"op": "expand_repeat", "group_id": "@flor"},
		map[string]interface{}{"op": "clear_repeat", "group_id": "g-1"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"repeat_layers"`, `"count":10`, `"start_angle":0`, `"upright":false`, `"group_id":"@flor"`, `"rows":2`, `"flip_rows":"none"`, `"op":"expand_repeat"`, `"op":"clear_repeat"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	repeat := edit.Definition().Parameters["operations"].Items.Properties["repeat"]
	if repeat.Items == nil || len(repeat.Items.Properties["mode"].Enum) != 3 || len(repeat.Items.Properties["flip_rows"].Enum) != 4 {
		t.Fatalf("repeat schema = %+v", repeat)
	}
}
