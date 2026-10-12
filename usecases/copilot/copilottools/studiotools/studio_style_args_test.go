package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioImageEditCarriesStyles(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "create_style", "ref": "h1", "style_kind": "text", "name": "Título", "role": "heading", "layer_id": "title", "link": true},
		map[string]interface{}{"op": "create_style", "ref": "red", "style_kind": "color", "name": "Vermelho", "style_values": map[string]interface{}{"color": "#e4003a"}},
		map[string]interface{}{"op": "apply_style", "style_id": "@red", "artboard_id": "story", "slot": "background"},
		map[string]interface{}{"op": "update_style", "style_id": "@h1", "style_values": map[string]interface{}{"font_size_px": 96.0, "italic": false}, "style_clear": []interface{}{"letter_spacing"}},
		map[string]interface{}{"op": "detach_style", "layer_ids": []interface{}{"title"}, "slot": "text"},
		map[string]interface{}{"op": "delete_style", "style_id": "@red"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"create_style"`, `"style_kind":"text"`, `"role":"heading"`, `"link":true`, `"style_values":{"color":"#e4003a"}`, `"slot":"background"`, `"font_size_px":96`, `"italic":false`, `"style_clear":["letter_spacing"]`, `"op":"detach_style"`, `"op":"delete_style"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	props := edit.Definition().Parameters["operations"].Items.Properties
	if strings.Join(props["slot"].Enum, ",") != "fill,stroke,text,effect,background" || strings.Join(props["style_kind"].Enum, ",") != "color,text,effect" {
		t.Fatalf("style schema = %+v / %+v", props["slot"], props["style_kind"])
	}
	if props["style_values"].Items == nil || props["style_values"].Items.Properties["font_size_px"].Type != "number" {
		t.Fatalf("style values schema = %+v", props["style_values"])
	}
	if !strings.Contains(studioImageEditGuide, "apply_style") || !strings.Contains(studioImageEditGuide, "update_style") {
		t.Fatal("the image guide does not explain styles")
	}
}
